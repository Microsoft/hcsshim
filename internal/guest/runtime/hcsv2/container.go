// +build linux

package hcsv2

import (
	"context"
	"sync"
	"syscall"

	"github.com/Microsoft/hcsshim/internal/guest/gcserr"
	"github.com/Microsoft/hcsshim/internal/guest/prot"
	"github.com/Microsoft/hcsshim/internal/guest/runtime"
	"github.com/Microsoft/hcsshim/internal/guest/stdio"
	"github.com/Microsoft/hcsshim/internal/guest/storage"
	"github.com/Microsoft/hcsshim/internal/guest/transport"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/containerd/cgroups"
	v1 "github.com/containerd/cgroups/stats/v1"
	oci "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"go.opencensus.io/trace"
)

type Container struct {
	id    string
	vsock transport.Transport

	spec      *oci.Spec
	isSandbox bool

	container   runtime.Container
	initProcess *containerProcess

	etL      sync.Mutex
	exitType prot.NotificationType

	processesMutex sync.Mutex
	processes      map[uint32]*containerProcess
}

func (c *Container) Start(ctx context.Context, conSettings stdio.ConnectionSettings) (int, error) {
	stdioSet, err := stdio.Connect(c.vsock, conSettings)
	if err != nil {
		return -1, err
	}
	if c.initProcess.spec.Terminal {
		ttyr := c.container.Tty()
		ttyr.ReplaceConnectionSet(stdioSet)
		ttyr.Start()
	} else {
		pr := c.container.PipeRelay()
		pr.ReplaceConnectionSet(stdioSet)
		pr.CloseUnusedPipes()
		pr.Start()
	}
	err = c.container.Start()
	if err != nil {
		stdioSet.Close()
	}
	return int(c.initProcess.pid), err
}

func (c *Container) ExecProcess(ctx context.Context, process *oci.Process, conSettings stdio.ConnectionSettings) (int, error) {
	stdioSet, err := stdio.Connect(c.vsock, conSettings)
	if err != nil {
		return -1, err
	}

	process.Rlimits = c.spec.Process.Rlimits
	p, err := c.container.ExecProcess(process, stdioSet)
	if err != nil {
		stdioSet.Close()
		return -1, err
	}

	pid := p.Pid()
	c.processesMutex.Lock()
	c.processes[uint32(pid)] = newProcess(c, process, p, uint32(pid), false)
	c.processesMutex.Unlock()
	return pid, nil
}

// GetProcess returns the Process with the matching 'pid'. If the 'pid' does
// not exit returns error.
func (c *Container) GetProcess(pid uint32) (Process, error) {
	if c.initProcess.pid == pid {
		return c.initProcess, nil
	}

	c.processesMutex.Lock()
	defer c.processesMutex.Unlock()

	p, ok := c.processes[pid]
	if !ok {
		return nil, gcserr.NewHresultError(gcserr.HrErrNotFound)
	}
	return p, nil
}

// GetAllProcessPids returns all process pids in the container namespace.
func (c *Container) GetAllProcessPids(ctx context.Context) ([]int, error) {
	state, err := c.container.GetAllProcesses()
	if err != nil {
		return nil, err
	}
	pids := make([]int, len(state))
	for i, s := range state {
		pids[i] = s.Pid
	}
	return pids, nil
}

// Kill sends 'signal' to the container process.
func (c *Container) Kill(ctx context.Context, signal syscall.Signal) error {
	err := c.container.Kill(signal)
	if err != nil {
		return err
	}
	c.setExitType(signal)
	return nil
}

func (c *Container) Delete(ctx context.Context) error {
	if c.isSandbox {
		// remove user mounts in sandbox container
		if err := storage.UnmountAllInPath(ctx, getSandboxMountsDir(c.id), true); err != nil {
			log.G(ctx).WithError(err).Error("failed to unmount sandbox mounts")
		}
	}
	return c.container.Delete()
}

func (c *Container) Update(ctx context.Context, resources interface{}) error {
	return c.container.Update(resources)
}

// Wait waits for the container's init process to exit.
func (c *Container) Wait() prot.NotificationType {
	_, span := trace.StartSpan(context.Background(), "opengcs::Container::Wait")
	defer span.End()
	span.AddAttributes(trace.StringAttribute("cid", c.id))

	c.initProcess.writersWg.Wait()
	c.etL.Lock()
	defer c.etL.Unlock()
	return c.exitType
}

// setExitType sets `c.exitType` to the appropriate value based on `signal` if
// `signal` will take down the container.
func (c *Container) setExitType(signal syscall.Signal) {
	c.etL.Lock()
	defer c.etL.Unlock()

	if signal == syscall.SIGTERM {
		c.exitType = prot.NtGracefulExit
	} else if signal == syscall.SIGKILL {
		c.exitType = prot.NtForcedExit
	}
}

// GetStats returns the cgroup metrics for the container.
func (c *Container) GetStats(ctx context.Context) (*v1.Metrics, error) {
	_, span := trace.StartSpan(ctx, "opengcs::Container::GetStats")
	defer span.End()
	span.AddAttributes(trace.StringAttribute("cid", c.id))

	cgroupPath := c.spec.Linux.CgroupsPath
	cg, err := cgroups.Load(cgroups.V1, cgroups.StaticPath(cgroupPath))
	if err != nil {
		return nil, errors.Errorf("failed to get container stats for %v: %v", c.id, err)
	}

	return cg.Stat(cgroups.IgnoreNotExist)
}

func (c *Container) modifyContainerConstraints(ctx context.Context, rt prot.ModifyRequestType, cc *prot.ContainerConstraintsV2) (err error) {
	return c.Update(ctx, cc.Linux)
}
