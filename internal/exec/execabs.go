package exec

import (
	"fmt"
	"path/filepath"
	"reflect"
	"unsafe"
)

func relError(file, path string) error {
	return fmt.Errorf("%s resolves to executable in current directory (.%c%s)", file, filepath.Separator, path)
}

// LookPath searches for an executable named file in the directories
// named by the PATH environment variable. If file contains a slash,
// it is tried directly and the PATH is not consulted. The result will be
// an absolute path.
//
// LookPath differs from exec.LookPath in its handling of PATH lookups,
// which are used for file names without slashes. If exec.LookPath's
// PATH lookup would have returned an executable from the current directory,
// LookPath instead returns an error.
func LookPath(file string) (string, error) {
	path, err := lookPath(file)
	if err != nil {
		return "", err
	}
	if filepath.Base(file) == file && !filepath.IsAbs(path) {
		return "", relError(file, path)
	}
	return path, nil
}

func fixCmd(name string, cmd *Cmd) {
	if filepath.Base(name) == name && !filepath.IsAbs(cmd.Path) {
		// exec.Command was called with a bare binary name and
		// exec.LookPath returned a path which is not absolute.
		// Set cmd.lookPathErr and clear cmd.Path so that it
		// cannot be run.
		lookPathErr := (*error)(unsafe.Pointer(reflect.ValueOf(cmd).Elem().FieldByName("lookPathErr").Addr().Pointer()))
		if *lookPathErr == nil {
			*lookPathErr = relError(name, cmd.Path)
		}
		cmd.Path = ""
	}
}

// Command returns the Cmd struct to execute the named program with the given arguments.
// See exec.Command for most details.
//
// Command differs from exec.Command in its handling of PATH lookups,
// which are used when the program name contains no slashes.
// If exec.Command would have returned an exec.Cmd configured to run an
// executable from the current directory, Command instead
// returns an exec.Cmd that will return an error from Start or Run.
func Command(name string, arg ...string) *Cmd {
	cmd := command(name, arg...)
	fixCmd(name, cmd)
	return cmd
}
