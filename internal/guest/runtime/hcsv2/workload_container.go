// +build linux

package hcsv2

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/oc"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runc/libcontainer/devices"
	oci "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"go.opencensus.io/trace"
)

func getWorkloadRootDir(id string) string {
	return filepath.Join("/run/gcs/c", id)
}

func updateSandboxMounts(sbid string, spec *oci.Spec) error {
	sandboxMountPrefix := "sandbox://"
	for i, m := range spec.Mounts {
		if strings.HasPrefix(m.Source, sandboxMountPrefix) {
			mountsDir := getSandboxMountsDir(sbid)
			subPath := strings.TrimPrefix(m.Source, sandboxMountPrefix)
			sandboxSource := filepath.Join(mountsDir, subPath)

			if !strings.HasPrefix(sandboxSource, mountsDir) {
				return errors.Errorf("mount path %v for mount %v is not within sandbox's mounts dir", sandboxSource, m.Source)
			}

			spec.Mounts[i].Source = sandboxSource

			_, err := os.Stat(sandboxSource)
			if os.IsNotExist(err) {
				if err := os.MkdirAll(sandboxSource, 0755); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func specHasGPUDevice(spec *oci.Spec) bool {
	for _, d := range spec.Windows.Devices {
		if d.IDType == "gpu" {
			return true
		}
	}
	return false
}

func setupWorkloadContainerSpec(ctx context.Context, sbid, id string, spec *oci.Spec) (err error) {
	ctx, span := trace.StartSpan(ctx, "hcsv2::setupWorkloadContainerSpec")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()
	span.AddAttributes(
		trace.StringAttribute("sandboxID", sbid),
		trace.StringAttribute("cid", id))

	// Verify no hostname
	if spec.Hostname != "" {
		return errors.Errorf("workload container must not change hostname: %s", spec.Hostname)
	}

	// update any sandbox mounts with the sandboxMounts directory path and create files
	if err = updateSandboxMounts(sbid, spec); err != nil {
		return errors.Wrapf(err, "failed to update sandbox mounts for container %v in sandbox %v", id, sbid)
	}

	// Add /etc/hostname if the spec did not override it.
	if !isInMounts("/etc/hostname", spec.Mounts) {
		mt := oci.Mount{
			Destination: "/etc/hostname",
			Type:        "bind",
			Source:      getSandboxHostnamePath(sbid),
			Options:     []string{"bind"},
		}
		if isRootReadonly(spec) {
			mt.Options = append(mt.Options, "ro")
		}
		spec.Mounts = append(spec.Mounts, mt)
	}

	// Add /etc/hosts if the spec did not override it.
	if !isInMounts("/etc/hosts", spec.Mounts) {
		mt := oci.Mount{
			Destination: "/etc/hosts",
			Type:        "bind",
			Source:      getSandboxHostsPath(sbid),
			Options:     []string{"bind"},
		}
		if isRootReadonly(spec) {
			mt.Options = append(mt.Options, "ro")
		}
		spec.Mounts = append(spec.Mounts, mt)
	}

	// Add /etc/resolv.conf if the spec did not override it.
	if !isInMounts("/etc/resolv.conf", spec.Mounts) {
		mt := oci.Mount{
			Destination: "/etc/resolv.conf",
			Type:        "bind",
			Source:      getSandboxResolvPath(sbid),
			Options:     []string{"bind"},
		}
		if isRootReadonly(spec) {
			mt.Options = append(mt.Options, "ro")
		}
		spec.Mounts = append(spec.Mounts, mt)
	}

	// TODO: JTERRY75 /dev/shm is not properly setup for LCOW I believe. CRI
	// also has a concept of a sandbox/shm file when the IPC NamespaceMode !=
	// NODE.

	// Check if we need to do any capability/device mappings
	if spec.Annotations["io.microsoft.virtualmachine.lcow.privileged"] == "true" {
		log.G(ctx).Debug("'io.microsoft.virtualmachine.lcow.privileged' set for privileged container")

		// Add all host devices
		hostDevices, err := devices.HostDevices()
		if err != nil {
			return err
		}
		for _, hostDevice := range hostDevices {
			addLinuxDeviceToSpec(ctx, hostDevice, spec, false)
		}

		// Set the cgroup access
		spec.Linux.Resources.Devices = []oci.LinuxDeviceCgroup{
			{
				Allow:  true,
				Access: "rwm",
			},
		}
	} else {
		tempLinuxDevices := spec.Linux.Devices
		spec.Linux.Devices = []oci.LinuxDevice{}
		for _, ld := range tempLinuxDevices {
			hostDevice, err := devices.DeviceFromPath(ld.Path, "rwm")
			if err != nil {
				return err
			}
			addLinuxDeviceToSpec(ctx, hostDevice, spec, true)
		}
	}

	if userstr, ok := spec.Annotations["io.microsoft.lcow.userstr"]; ok {
		if err := setUserStr(spec, userstr); err != nil {
			return err
		}
	}

	if rlimCore := spec.Annotations["io.microsoft.lcow.rlimitcore"]; rlimCore != "" {
		if err := setCoreRLimit(spec, rlimCore); err != nil {
			return err
		}
	}

	// Force the parent cgroup into our /containers root
	spec.Linux.CgroupsPath = "/containers/" + id

	if spec.Windows != nil && specHasGPUDevice(spec) {
		// we only support Nvidia gpus right now
		ldConfigargs := []string{"-l", "/run/nvidia/lib"}
		env := updateEnvWithNvidiaVariables()
		if err := addLDConfigHook(ctx, spec, ldConfigargs, env); err != nil {
			return err
		}
		if err := addNvidiaDevicePreHook(ctx, spec); err != nil {
			return err
		}
	}

	// Clear the windows section as we dont want to forward to runc
	spec.Windows = nil

	return nil
}

// Helper function to create an oci prestart hook to run ldconfig
func addLDConfigHook(ctx context.Context, spec *oci.Spec, args, env []string) error {
	if spec.Hooks == nil {
		spec.Hooks = &oci.Hooks{}
	}

	ldConfigHook := oci.Hook{
		Path: "/sbin/ldconfig",
		Args: args,
		Env:  env,
	}

	spec.Hooks.Prestart = append(spec.Hooks.Prestart, ldConfigHook)
	return nil
}

func addLinuxDeviceToSpec(ctx context.Context, hostDevice *configs.Device, spec *oci.Spec, addCgroupDevice bool) {
	rd := oci.LinuxDevice{
		Path:  hostDevice.Path,
		Type:  string(hostDevice.Type),
		Major: hostDevice.Major,
		Minor: hostDevice.Minor,
		UID:   &hostDevice.Uid,
		GID:   &hostDevice.Gid,
	}
	if hostDevice.Major == 0 && hostDevice.Minor == 0 {
		// Invalid device, most likely a symbolic link, skip it.
		return
	}
	found := false
	for i, dev := range spec.Linux.Devices {
		if dev.Path == rd.Path {
			found = true
			spec.Linux.Devices[i] = rd
			break
		}
		if dev.Type == rd.Type && dev.Major == rd.Major && dev.Minor == rd.Minor {
			log.G(ctx).Warnf("The same type '%s', major '%d' and minor '%d', should not be used for multiple devices.", dev.Type, dev.Major, dev.Minor)
		}
	}
	if !found {
		spec.Linux.Devices = append(spec.Linux.Devices, rd)
		if addCgroupDevice {
			deviceCgroup := oci.LinuxDeviceCgroup{
				Allow:  true,
				Type:   string(hostDevice.Type),
				Major:  &hostDevice.Major,
				Minor:  &hostDevice.Minor,
				Access: string(hostDevice.Permissions),
			}
			spec.Linux.Resources.Devices = append(spec.Linux.Resources.Devices, deviceCgroup)
		}
	}
}
