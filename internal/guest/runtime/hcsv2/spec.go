// +build linux

package hcsv2

import (
	"context"
	"fmt"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/opencontainers/runc/libcontainer/devices"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/opencontainers/runc/libcontainer/user"
	oci "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

// getNetworkNamespaceID returns the `ToLower` of
// `spec.Windows.Network.NetworkNamespace` or `""`.
func getNetworkNamespaceID(spec *oci.Spec) string {
	if spec.Windows != nil &&
		spec.Windows.Network != nil {
		return strings.ToLower(spec.Windows.Network.NetworkNamespace)
	}
	return ""
}

// isRootReadonly returns `true` if the spec specifies the rootfs is readonly.
func isRootReadonly(spec *oci.Spec) bool {
	if spec.Root != nil {
		return spec.Root.Readonly
	}
	return false
}

// isInMounts returns `true` if `target` matches a `Destination` in any of
// `mounts`.
func isInMounts(target string, mounts []oci.Mount) bool {
	for _, m := range mounts {
		if m.Destination == target {
			return true
		}
	}
	return false
}

// removeMount removes mount from the array if `target` matches `Destination`
func removeMount(target string, mounts []oci.Mount) []oci.Mount {
	var result []oci.Mount
	for _, m := range mounts {
		if m.Destination == target {
			continue
		}
		result = append(result, m)
	}
	return result
}

func setProcess(spec *oci.Spec) {
	if spec.Process == nil {
		spec.Process = &oci.Process{}
	}
}

// setUserStr sets `spec.Process` to the valid `userstr` based on the OCI Image Spec
// v1.0.0 `userstr`.
//
// Valid values are: user, uid, user:group, uid:gid, uid:group, user:gid.
// If uid is provided instead of the username then that value is not checked against the
// /etc/passwd file to verify if the user with given uid actually exists.
func setUserStr(spec *oci.Spec, userstr string) error {
	setProcess(spec)

	parts := strings.Split(userstr, ":")
	switch len(parts) {
	case 1:
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			// evaluate username to uid/gid
			return setUsername(spec, userstr)
		}
		return setUserID(spec, int(v))
	case 2:
		var (
			username, groupname string
			uid, gid            int
		)
		v, err := strconv.Atoi(parts[0])
		if err != nil {
			username = parts[0]
		} else {
			uid = int(v)
		}
		v, err = strconv.Atoi(parts[1])
		if err != nil {
			groupname = parts[1]
		} else {
			gid = int(v)
		}
		if username != "" {
			u, err := getUser(spec, func(u user.User) bool {
				return u.Name == username
			})
			if err != nil {
				return errors.Wrapf(err, "failed to find user by username: %s", username)
			}
			uid = u.Uid
		}
		if groupname != "" {
			g, err := getGroup(spec, func(g user.Group) bool {
				return g.Name == groupname
			})
			if err != nil {
				return errors.Wrapf(err, "failed to find group by groupname: %s", groupname)
			}
			gid = g.Gid
		}
		spec.Process.User.UID, spec.Process.User.GID = uint32(uid), uint32(gid)
		return nil
	default:
		return fmt.Errorf("invalid userstr: '%s'", userstr)
	}
}

func setUsername(spec *oci.Spec, username string) error {
	u, err := getUser(spec, func(u user.User) bool {
		return u.Name == username
	})
	if err != nil {
		return errors.Wrapf(err, "failed to find user by username: %s", username)
	}
	spec.Process.User.UID, spec.Process.User.GID = uint32(u.Uid), uint32(u.Gid)
	return nil
}

func setUserID(spec *oci.Spec, uid int) error {
	u, err := getUser(spec, func(u user.User) bool {
		return u.Uid == uid
	})
	if err != nil {
		spec.Process.User.UID, spec.Process.User.GID = uint32(uid), 0
		return nil
	}
	spec.Process.User.UID, spec.Process.User.GID = uint32(u.Uid), uint32(u.Gid)
	return nil
}

func getUser(spec *oci.Spec, filter func(user.User) bool) (user.User, error) {
	users, err := user.ParsePasswdFileFilter(filepath.Join(spec.Root.Path, "/etc/passwd"), filter)
	if err != nil {
		return user.User{}, err
	}
	if len(users) != 1 {
		return user.User{}, errors.Errorf("expected exactly 1 user matched '%d'", len(users))
	}
	return users[0], nil
}

func getGroup(spec *oci.Spec, filter func(user.Group) bool) (user.Group, error) {
	groups, err := user.ParseGroupFileFilter(filepath.Join(spec.Root.Path, "/etc/group"), filter)
	if err != nil {
		return user.Group{}, err
	}
	if len(groups) != 1 {
		return user.Group{}, errors.Errorf("expected exactly 1 group matched '%d'", len(groups))
	}
	return groups[0], nil
}

// applyAnnotationsToSpec modifies the spec based on additional information from annotations
func applyAnnotationsToSpec(ctx context.Context, spec *oci.Spec) error {
	// Check if we need to override container's /dev/shm
	if val, ok := spec.Annotations["io.microsoft.container.storage.shm.size-kb"]; ok {
		sz, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			return errors.Wrap(err, "/dev/shm size must be a valid integer")
		}
		if sz <= 0 {
			return errors.Errorf("/dev/shm size must be a positive integer, got: %d", sz)
		}

		size := fmt.Sprintf("size=%dk", sz)
		mt := oci.Mount{
			Destination: "/dev/shm",
			Type:        "tmpfs",
			Source:      "shm",
			Options:     []string{"nosuid", "noexec", "nodev", "mode=1777", size},
		}
		spec.Mounts = removeMount("/dev/shm", spec.Mounts)
		spec.Mounts = append(spec.Mounts, mt)
	}

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

	// Check if we need to set non-default user
	if userstr, ok := spec.Annotations["io.microsoft.lcow.userstr"]; ok {
		if err := setUserStr(spec, userstr); err != nil {
			return err
		}
	}
	return nil
}
