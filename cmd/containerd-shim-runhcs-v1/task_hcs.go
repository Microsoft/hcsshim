package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Microsoft/hcsshim/internal/hcs"
	"github.com/Microsoft/hcsshim/internal/hcsoci"
	"github.com/Microsoft/hcsshim/internal/oci"
	"github.com/Microsoft/hcsshim/internal/uvm"
	"github.com/Microsoft/hcsshim/osversion"
	eventstypes "github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/runtime"
	"github.com/containerd/containerd/runtime/v2/task"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

func newHcsStandaloneTask(ctx context.Context, events publisher, req *task.CreateTaskRequest, s *specs.Spec) (shimTask, error) {
	logrus.WithFields(logrus.Fields{
		"tid": req.ID,
	}).Debug("newHcsStandloneTask")

	ct, _, err := oci.GetSandboxTypeAndID(s.Annotations)
	if err != nil {
		return nil, err
	}
	if ct != oci.KubernetesContainerTypeNone {
		return nil, errors.Wrapf(
			errdefs.ErrFailedPrecondition,
			"cannot create standalone task, expected no annotation: '%s': got '%s'",
			oci.KubernetesContainerTypeAnnotation,
			ct)
	}

	owner := filepath.Base(os.Args[0])

	var parent *uvm.UtilityVM
	if osversion.Get().Build >= osversion.RS5 && oci.IsIsolated(s) {
		// Create the UVM parent
		opts, err := oci.SpecToUVMCreateOpts(s, fmt.Sprintf("%s@vm", req.ID), owner)
		if err != nil {
			return nil, err
		}
		switch opts.(type) {
		case *uvm.OptionsLCOW:
			lopts := (opts).(*uvm.OptionsLCOW)
			parent, err = uvm.CreateLCOW(lopts)
			if err != nil {
				return nil, err
			}
		case *uvm.OptionsWCOW:
			wopts := (opts).(*uvm.OptionsWCOW)

			// In order for the UVM sandbox.vhdx not to collide with the actual
			// nested Argon sandbox.vhdx we append the \vm folder to the last
			// entry in the list.
			layersLen := len(s.Windows.LayerFolders)
			layers := make([]string, layersLen)
			copy(layers, s.Windows.LayerFolders)

			vmPath := filepath.Join(layers[layersLen-1], "vm")
			err := os.MkdirAll(vmPath, 0)
			if err != nil {
				return nil, err
			}
			layers[layersLen-1] = vmPath
			wopts.LayerFolders = layers

			parent, err = uvm.CreateWCOW(wopts)
			if err != nil {
				return nil, err
			}
		}
		err = parent.Start()
		if err != nil {
			parent.Close()
		}
	} else if !oci.IsWCOW(s) {
		return nil, errors.Wrap(errdefs.ErrFailedPrecondition, "oci spec does not contain WCOW or LCOW spec")
	}

	shim, err := newHcsTask(ctx, events, parent, true, req, s)
	if err != nil {
		if parent != nil {
			parent.Close()
		}
		return nil, err
	}
	return shim, nil
}

// newHcsTask creates a container within `parent` and its init exec process in
// the `shimExecCreated` state and returns the task that tracks its lifetime.
//
// If `parent == nil` the container is created on the host.
func newHcsTask(
	ctx context.Context,
	events publisher,
	parent *uvm.UtilityVM,
	ownsParent bool,
	req *task.CreateTaskRequest,
	s *specs.Spec) (shimTask, error) {
	logrus.WithFields(logrus.Fields{
		"tid":        req.ID,
		"ownsParent": ownsParent,
	}).Debug("newHcsTask")

	owner, err := os.Executable()
	if err != nil {
		return nil, err
	}

	io, err := newRelay(ctx, req.Stdin, req.Stdout, req.Stderr, req.Terminal)
	if err != nil {
		return nil, err
	}

	var netNS string
	if s.Windows != nil &&
		s.Windows.Network != nil {
		netNS = s.Windows.Network.NetworkNamespace
	}
	opts := hcsoci.CreateOptions{
		ID:               req.ID,
		Owner:            owner,
		Spec:             s,
		HostingSystem:    parent,
		NetworkNamespace: netNS,
	}
	system, resources, err := hcsoci.CreateContainer(&opts)
	if err != nil {
		return nil, err
	}

	ht := &hcsTask{
		events:   events,
		id:       req.ID,
		isWCOW:   oci.IsWCOW(s),
		c:        system,
		cr:       resources,
		ownsHost: ownsParent,
		host:     parent,
	}
	ht.init = newHcsExec(
		ctx,
		events,
		req.ID,
		parent,
		system,
		req.ID,
		req.Bundle,
		ht.isWCOW,
		s.Process,
		io)

	// Issue the background wait
	go ht.waitForExit()

	// Publish the created event
	ht.events(
		runtime.TaskCreateEventTopic,
		&eventstypes.TaskCreate{
			ContainerID: req.ID,
			Bundle:      req.Bundle,
			Rootfs:      req.Rootfs,
			IO: &eventstypes.TaskIO{
				Stdin:    req.Stdin,
				Stdout:   req.Stdout,
				Stderr:   req.Stderr,
				Terminal: req.Terminal,
			},
			Checkpoint: "",
			Pid:        uint32(ht.init.Pid()),
		})
	return ht, nil
}

var _ = (shimTask)(&hcsTask{})

// hcsTask is a generic task that represents a WCOW Container (process or
// hypervisor isolated), or a LCOW Container. This task MAY own the UVM the
// container is in but in the case of a POD it may just track the UVM for
// container lifetime management. In the case of ownership when the init
// task/exec is stopped the UVM itself will be stopped as well.
type hcsTask struct {
	events publisher
	// id is the id of this task when it is created.
	//
	// It MUST be treated as read only in the liftetime of the task.
	id string
	// isWCOW is set to `true` if this is a task representing a Windows container.
	//
	// It MUST be treated as read only in the liftetime of the task.
	isWCOW bool
	// c is the container backing this task.
	//
	// It MUST be treated as read only in the lifetime of this task EXCEPT after
	// a Kill to the init task in which it must be shutdown.
	c *hcs.System
	// cr is the container resources this task is holding.
	//
	// It MUST be treated as read only in the lifetime of this task EXCEPT after
	// a Kill to the init task in which all resources must be released.
	cr *hcsoci.Resources
	// init is the init process of the container.
	//
	// Note: the invariant `container state == init.State()` MUST be true. IE:
	// if the init process exits the container as a whole and all exec's MUST
	// exit.
	//
	// It MUST be treated as read only in the lifetime of the task.
	init shimExec
	// ownsHost is `true` if this task owns `host`. If so when this tasks init
	// exec shuts down it is required that `host` be shut down as well.
	ownsHost bool
	// host is the hosting VM for this exec if hypervisor isolated. If
	// `host==nil` this is an Argon task so no UVM cleanup is required.
	//
	// NOTE: if `osversion.Get().Build < osversion.RS5` this will always be
	// `nil`.
	host *uvm.UtilityVM

	// ecl is the exec create lock for all non-init execs and MUST be held
	// durring create to prevent ID duplication.
	ecl   sync.Mutex
	execs sync.Map
}

func (ht *hcsTask) ID() string {
	return ht.id
}

func (ht *hcsTask) CreateExec(ctx context.Context, req *task.ExecProcessRequest, spec *specs.Process) error {
	logrus.WithFields(logrus.Fields{
		"tid": ht.id,
		"eid": req.ExecID,
	}).Debug("hcsTask::CreateExec")

	ht.ecl.Lock()
	defer ht.ecl.Unlock()

	// If the task exists or we got a request for "" which is the init task
	// fail.
	if _, loaded := ht.execs.Load(req.ExecID); loaded || req.ExecID == "" {
		return errors.Wrapf(errdefs.ErrAlreadyExists, "exec: '%s' in task: '%s' already exists", req.ExecID, ht.id)
	}

	if ht.init.State() != shimExecStateRunning {
		return errors.Wrapf(errdefs.ErrFailedPrecondition, "exec: '' in task: '%s' must be running to create additional execs", ht.id)
	}

	io, err := newRelay(ctx, req.Stdin, req.Stdout, req.Stderr, req.Terminal)
	if err != nil {
		return err
	}
	he := newHcsExec(ctx, ht.events, ht.id, ht.host, ht.c, req.ExecID, ht.init.Status().Bundle, ht.isWCOW, spec, io)
	ht.execs.Store(req.ExecID, he)

	// Publish the created event
	ht.events(
		runtime.TaskExecAddedEventTopic,
		&eventstypes.TaskExecAdded{
			ContainerID: ht.id,
			ExecID:      req.ExecID,
		})

	return nil
}

func (ht *hcsTask) GetExec(eid string) (shimExec, error) {
	if eid == "" {
		return ht.init, nil
	}
	raw, loaded := ht.execs.Load(eid)
	if !loaded {
		return nil, errors.Wrapf(errdefs.ErrNotFound, "exec: '%s' in task: '%s' not found", eid, ht.id)
	}
	return raw.(shimExec), nil
}

func (ht *hcsTask) KillExec(ctx context.Context, eid string, signal uint32, all bool) error {
	logrus.WithFields(logrus.Fields{
		"tid":    ht.id,
		"eid":    eid,
		"signal": signal,
		"all":    all,
	}).Debug("hcsTask::KillExec")

	e, err := ht.GetExec(eid)
	if err != nil {
		return err
	}
	if all && eid != "" {
		return errors.Wrapf(errdefs.ErrFailedPrecondition, "cannot signal all for non-empty exec: '%s'", eid)
	}
	eg := errgroup.Group{}
	if all {
		// We are in a kill all on the init task. Signal everything.
		ht.execs.Range(func(key, value interface{}) bool {
			ex := value.(shimExec)
			eg.Go(func() error {
				return ex.Kill(ctx, signal)
			})

			// iterate all
			return false
		})
	} else if eid == "" {
		// We are in a kill of the init task. Verify all exec's are in the
		// non-running state.
		invalid := false
		ht.execs.Range(func(key, value interface{}) bool {
			ex := value.(shimExec)
			if ex.State() != shimExecStateExited {
				invalid = true
				// we have an invalid state. Stop iteration.
				return true
			}
			// iterate next valid
			return false
		})
		if invalid {
			return errors.Wrap(errdefs.ErrFailedPrecondition, "cannot signal init exec with un-exited additional exec's")
		}
	}
	eg.Go(func() error {
		return e.Kill(ctx, signal)
	})
	err = eg.Wait()
	if err != nil {
		return err
	}
	return nil
}

func (ht *hcsTask) DeleteExec(ctx context.Context, eid string) (int, uint32, time.Time, error) {
	logrus.WithFields(logrus.Fields{
		"tid": ht.id,
		"eid": eid,
	}).Debug("hcsTask::DeleteExec")

	e, err := ht.GetExec(eid)
	if err != nil {
		return 0, 0, time.Time{}, err
	}
	if eid == "" {
		// We are deleting the init exec. Verify all additional exec's are exited as well
		invalid := false
		ht.execs.Range(func(key, value interface{}) bool {
			ex := value.(shimExec)
			if ex.State() != shimExecStateExited {
				invalid = true
				// we have an invalid state. Stop iteration.
				return true
			}
			// iterate next valid
			return false
		})
		if invalid {
			return 0, 0, time.Time{}, errors.Wrap(errdefs.ErrFailedPrecondition, "cannot delete init exec with un-exited additional exec's")
		}
	}
	state := e.State()
	if state != shimExecStateExited {
		return 0, 0, time.Time{}, newExecInvalidStateError(ht.id, eid, state, "delete")
	}
	status := e.Status()
	if eid != "" {
		ht.execs.Delete(eid)
	}

	// Publish the deleted event
	ht.events(
		runtime.TaskDeleteEventTopic,
		&eventstypes.TaskDelete{
			ContainerID: ht.id,
			ID:          eid,
			Pid:         status.Pid,
			ExitStatus:  status.ExitStatus,
			ExitedAt:    status.ExitedAt,
		})

	return int(status.Pid), status.ExitStatus, status.ExitedAt, nil
}

func (ht *hcsTask) Pids(ctx context.Context) ([]shimTaskPidPair, error) {
	logrus.WithFields(logrus.Fields{
		"tid": ht.id,
	}).Debug("hcsTask::Pids")

	return nil, errdefs.ErrNotImplemented
}

// waitForExit waits for the init process exit and shuts down the container that
// is owned by this task and if `ht.ownsHost` will shutdown the hosting VM the
// container was placed in.
//
// NOTE: For Windows process isolated containers `ht.ownsHost==true && ht.host
// == nil`.
func (ht *hcsTask) waitForExit() {
	logrus.WithFields(logrus.Fields{
		"tid": ht.id,
	}).Debug("hcsTask::waitForExit")

	// Wait for the init task to exit.
	ht.init.Wait(context.Background())

	// ht.c should never be nil for a real task but in testing we stub
	// this to avoid a nil dereference. We really should introduce a
	// method or interface for ht.c operations that we can stub for
	// testing.
	if ht.c != nil {
		// Do our best attempt to tear down the container.
		if err := ht.c.Shutdown(); err != nil {
			if !hcs.IsPending(err) {
				logrus.WithFields(logrus.Fields{
					"tid":           ht.id,
					logrus.ErrorKey: err,
				}).Error("hcsTask::close - failed to shutdown container")
			} else {
				const shutdownTimeout = time.Minute * 5
				if err := ht.c.WaitTimeout(shutdownTimeout); err != nil {
					logrus.WithFields(logrus.Fields{
						"tid":           ht.id,
						logrus.ErrorKey: err,
					}).Error("hcsTask::close - failed to wait for container shutdown")
				}
			}
			if err := ht.c.Terminate(); err != nil && !hcs.IsAlreadyStopped(err) {
				if !hcs.IsPending(err) {
					logrus.WithFields(logrus.Fields{
						"tid":           ht.id,
						logrus.ErrorKey: err,
					}).Error("hcsTask::close - failed to terminate container")
				} else {
					const terminateTimeout = time.Second * 30
					if err := ht.c.WaitTimeout(terminateTimeout); err != nil {
						logrus.WithFields(logrus.Fields{
							"tid":           ht.id,
							logrus.ErrorKey: err,
						}).Error("hcsTask::close - failed to wait for container terminate")
					}
				}
			}
		}

		// Release any resources associated with the container.
		if err := hcsoci.ReleaseResources(ht.cr, ht.host, true); err != nil {
			logrus.WithFields(logrus.Fields{
				"tid":           ht.id,
				logrus.ErrorKey: err,
			}).Error("hcsTask::close - failed to release container resources")
		}

		// Close the container handle invalidating all future access.
		if err := ht.c.Close(); err != nil && !hcs.IsAlreadyClosed(err) {
			logrus.WithFields(logrus.Fields{
				"tid":           ht.id,
				logrus.ErrorKey: err,
			}).Error("hcsTask::close - failed to close container")
		}
	}

	if ht.ownsHost && ht.host != nil {
		logrus.WithFields(logrus.Fields{
			"tid": ht.id,
		}).Debug("hcsTask::close - begin host vm shutdown")

		if err := ht.host.Close(); err != nil {
			logrus.WithFields(logrus.Fields{
				"tid":           ht.id,
				logrus.ErrorKey: err,
			}).Error("hcsTask::close - failed host vm shutdown")
		}
	}
}
