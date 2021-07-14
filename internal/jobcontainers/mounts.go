package jobcontainers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

// namedPipePath returns true if the given path is to a named pipe.
func isnamedPipePath(p string) bool {
	return strings.HasPrefix(p, `\\.\pipe\`)
}

// Strip the drive letter (if there is one) so we don't end up with "%CONTAINER_SANDBOX_MOUNT_POINT%"\C:\path\to\mount
func stripDriveLetter(name string) string {
	// Remove drive letter
	if len(name) == 2 && name[1] == ':' {
		name = "."
	} else if len(name) > 2 && name[1] == ':' {
		name = name[2:]
	}
	return name
}

// setupMounts adds the custom mounts requested in the OCI runtime spec. Mounts are a bit funny as you already have
// access to everything on the host, so just symlink in whatever was requested to the path where the container volume
// is mounted. At least then the mount can be accessed from a path relative to the default working directory/where the volume
// is.
func setupMounts(spec *specs.Spec, sandboxVolumePath string) error {
	for _, mount := range spec.Mounts {
		if mount.Destination == "" || mount.Source == "" {
			return fmt.Errorf("invalid OCI spec - a mount must have both source and a destination: %+v", mount)
		}

		if isnamedPipePath(mount.Source) {
			return errors.New("named pipe mounts not supported for job containers - interact with the pipe directly")
		}

		fullCtrPath := filepath.Join(sandboxVolumePath, stripDriveLetter(mount.Destination))
		// Make sure all of the dirs leading up to the full path exist.
		strippedCtrPath := filepath.Dir(fullCtrPath)
		if err := os.MkdirAll(strippedCtrPath, 0777); err != nil {
			return errors.Wrap(err, "failed to make directory for job container mount")
		}

		if err := os.Symlink(mount.Source, fullCtrPath); err != nil {
			return errors.Wrap(err, "failed to setup mount for job container")
		}
	}

	return nil
}
