// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package exec

import (
	"errors"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/Microsoft/hcsshim/internal/winapi"
	"golang.org/x/sys/windows"
)

// args hold the command-line arguments, starting with the program name.
var Args []string

// OsProcAttr holds the attributes that will be applied to a new process
// started by StartProcess. This is actually os.ProcAttr but since we need
// windows.ProcAttr defined here as well we need to rename this.
type OsProcAttr struct {
	// If Dir is non-empty, the child changes into the directory before
	// creating the process.
	Dir string
	// If Env is non-nil, it gives the environment variables for the
	// new process in the form returned by Environ.
	// If it is nil, the result of Environ will be used.
	Env []string
	// Files specifies the open files inherited by the new process. The
	// first three entries correspond to standard input, standard output, and
	// standard error. An implementation may support additional entries,
	// depending on the underlying operating system. A nil entry corresponds
	// to that file being closed when the process starts.
	Files []*os.File

	// Operating system-specific process creation attributes.
	// Note that setting this field means that your program
	// may not execute properly or even compile on some
	// operating systems.
	Sys *SysProcAttr
}

type ProcAttr struct {
	Dir   string
	Env   []string
	Files []uintptr
	Sys   *SysProcAttr
}

type SysProcAttr struct {
	HideWindow         bool
	CmdLine            string // used if non-empty, else the windows command line is built by escaping the arguments passed to StartProcess
	CreationFlags      uint32
	Token              windows.Token                    // if set, runs new process in the security context represented by the token
	ProcessAttributes  *windows.SecurityAttributes      // if set, applies these security attributes as the descriptor for the new process
	ThreadAttributes   *windows.SecurityAttributes      // if set, applies these security attributes as the descriptor for the main thread of the new process
	ProcThreadAttrList *windows.ProcThreadAttributeList // if set, applies these process thread attributes for the new process.
}

// Process stores the information about a process created by StartProcess.
type Process struct {
	Pid    int
	handle uintptr // handle is accessed atomically on Windows
	isdone uint32  // process has been successfully waited on, non zero if true
}

func newProcess(pid int, handle uintptr) *Process {
	p := &Process{Pid: pid, handle: handle}
	runtime.SetFinalizer(p, (*Process).Release)
	return p
}

func environForSysProcAttr(sys *SysProcAttr) (env []string, err error) {
	if sys == nil || sys.Token == 0 {
		return os.Environ(), nil
	}
	var block *uint16
	err = windows.CreateEnvironmentBlock(&block, windows.Token(sys.Token), false)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = windows.DestroyEnvironmentBlock(block)
	}()
	blockp := uintptr(unsafe.Pointer(block))
	for {
		entry := (*[(1 << 30) - 1]uint16)(unsafe.Pointer(blockp))[:]
		for i, v := range entry {
			if v == 0 {
				entry = entry[:i]
				break
			}
		}
		if len(entry) == 0 {
			break
		}
		env = append(env, string(utf16.Decode(entry)))
		blockp += 2 * (uintptr(len(entry)) + 1)
	}
	return
}

func StartProcess(name string, argv []string, attr *OsProcAttr) (p *Process, err error) {
	// If there is no SysProcAttr (ie. no Chroot or changed
	// UID/GID), double-check existence of the directory we want
	// to chdir into. We can make the error clearer this way.
	if attr != nil && attr.Sys == nil && attr.Dir != "" {
		if _, err := os.Stat(attr.Dir); err != nil {
			pe := err.(*os.PathError)
			pe.Op = "chdir"
			return nil, pe
		}
	}

	sysattr := &ProcAttr{
		Dir: attr.Dir,
		Env: attr.Env,
		Sys: attr.Sys,
	}
	if sysattr.Env == nil {
		sysattr.Env, err = environForSysProcAttr(sysattr.Sys)
		if err != nil {
			return nil, err
		}
	}
	sysattr.Files = make([]uintptr, 0, len(attr.Files))
	for _, f := range attr.Files {
		sysattr.Files = append(sysattr.Files, f.Fd())
	}

	pid, h, e := startProcess(name, argv, sysattr)
	if e != nil {
		return nil, &os.PathError{Op: "fork/exec", Path: name, Err: e}
	}
	return newProcess(pid, h), nil
}

var zeroProcAttr ProcAttr
var zeroSysProcAttr SysProcAttr

func startProcess(argv0 string, argv []string, attr *ProcAttr) (pid int, handle uintptr, err error) {
	if len(argv0) == 0 {
		return 0, 0, syscall.EWINDOWS
	}
	if attr == nil {
		attr = &zeroProcAttr
	}
	sys := attr.Sys
	if sys == nil {
		sys = &zeroSysProcAttr
	}

	if len(attr.Files) > 3 {
		return 0, 0, syscall.EWINDOWS
	}
	if len(attr.Files) < 3 {
		return 0, 0, syscall.EINVAL
	}

	if len(attr.Dir) != 0 {
		// StartProcess assumes that argv0 is relative to attr.Dir,
		// because it implies Chdir(attr.Dir) before executing argv0.
		// Windows CreateProcess assumes the opposite: it looks for
		// argv0 relative to the current directory, and, only once the new
		// process is started, it does Chdir(attr.Dir). We are adjusting
		// for that difference here by making argv0 absolute.
		var err error
		argv0, err = joinExeDirAndFName(attr.Dir, argv0)
		if err != nil {
			return 0, 0, err
		}
	}
	argv0p, err := windows.UTF16PtrFromString(argv0)
	if err != nil {
		return 0, 0, err
	}

	var cmdline string
	// Windows CreateProcess takes the command line as a single string:
	// use attr.CmdLine if set, else build the command line by escaping
	// and joining each argument with spaces
	if sys.CmdLine != "" {
		cmdline = sys.CmdLine
	} else {
		cmdline = makeCmdLine(argv)
	}

	var argvp *uint16
	if len(cmdline) != 0 {
		argvp, err = windows.UTF16PtrFromString(cmdline)
		if err != nil {
			return 0, 0, err
		}
	}

	var dirp *uint16
	if len(attr.Dir) != 0 {
		dirp, err = windows.UTF16PtrFromString(attr.Dir)
		if err != nil {
			return 0, 0, err
		}
	}

	// Acquire the fork lock so that no other threads
	// create new fds that are not yet close-on-exec
	// before we fork.
	ForkLock.Lock()
	defer ForkLock.Unlock()

	p := windows.CurrentProcess()
	fd := make([]windows.Handle, len(attr.Files))
	for i := range attr.Files {
		if attr.Files[i] > 0 {
			err := windows.DuplicateHandle(p, windows.Handle(attr.Files[i]), p, &fd[i], 0, true, windows.DUPLICATE_SAME_ACCESS)
			if err != nil {
				return 0, 0, err
			}
			defer func() {
				_ = windows.CloseHandle(windows.Handle(fd[i]))
			}()
		}
	}

	si := new(windows.StartupInfoEx)
	si.StartupInfo.Cb = uint32(unsafe.Sizeof(*si))
	si.StartupInfo.Flags = windows.STARTF_USESTDHANDLES
	if sys.HideWindow {
		si.StartupInfo.Flags |= windows.STARTF_USESHOWWINDOW
		si.StartupInfo.ShowWindow = windows.SW_HIDE
	}
	si.StartupInfo.StdInput = fd[0]
	si.StartupInfo.StdOutput = fd[1]
	si.StartupInfo.StdErr = fd[2]
	si.ProcThreadAttributeList = sys.ProcThreadAttrList

	pi := new(windows.ProcessInformation)

	flags := sys.CreationFlags | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT
	if sys.Token != 0 {
		err = winapi.CreateProcessAsUser(sys.Token, argv0p, argvp, sys.ProcessAttributes, sys.ThreadAttributes, true, flags, createEnvBlock(attr.Env), dirp, &si.StartupInfo, pi)
	} else {
		err = windows.CreateProcess(argv0p, argvp, sys.ProcessAttributes, sys.ThreadAttributes, true, flags, createEnvBlock(attr.Env), dirp, &si.StartupInfo, pi)
	}
	if err != nil {
		return 0, 0, err
	}
	defer func() {
		_ = windows.CloseHandle(windows.Handle(pi.Thread))
	}()
	return int(pi.ProcessId), uintptr(pi.Process), nil
}

// Release releases any resources associated with the Process p,
// rendering it unusable in the future.
// Release only needs to be called if Wait is not.
func (p *Process) Release() error {
	return p.release()
}

// Kill causes the Process to exit immediately. Kill does not wait until
// the Process has actually exited. This only kills the Process itself,
// not any other processes it may have started.
func (p *Process) Kill() error {
	return p.kill()
}

func (p *Process) kill() error {
	return p.Signal(os.Kill)
}

// Wait waits for the Process to exit, and then returns a
// ProcessState describing its status and an error, if any.
// Wait releases any resources associated with the Process.
// On most operating systems, the Process must be a child
// of the current process or an error will be returned.
func (p *Process) Wait() (*ProcessState, error) {
	return p.wait()
}

// Signal sends a signal to the Process.
// Sending Interrupt on Windows is not implemented.
func (p *Process) Signal(sig os.Signal) error {
	return p.signal(sig)
}

func (p *Process) wait() (ps *ProcessState, err error) {
	handle := atomic.LoadUintptr(&p.handle)
	s, e := windows.WaitForSingleObject(windows.Handle(handle), windows.INFINITE)
	switch s {
	case windows.WAIT_OBJECT_0:
		break
	case windows.WAIT_FAILED:
		return nil, os.NewSyscallError("WaitForSingleObject", e)
	default:
		return nil, errors.New("os: unexpected result from WaitForSingleObject")
	}
	var ec uint32
	e = windows.GetExitCodeProcess(windows.Handle(handle), &ec)
	if e != nil {
		return nil, os.NewSyscallError("GetExitCodeProcess", e)
	}
	var u windows.Rusage
	e = windows.GetProcessTimes(windows.Handle(handle), &u.CreationTime, &u.ExitTime, &u.KernelTime, &u.UserTime)
	if e != nil {
		return nil, os.NewSyscallError("GetProcessTimes", e)
	}
	p.setDone()
	// NOTE(brainman): It seems that sometimes process is not dead
	// when WaitForSingleObject returns. But we do not know any
	// other way to wait for it. Sleeping for a while seems to do
	// the trick sometimes.
	// See https://golang.org/issue/25965 for details.
	defer time.Sleep(5 * time.Millisecond)
	defer func() {
		_ = p.Release()
	}()
	return &ProcessState{p.Pid, windows.WaitStatus{ExitCode: ec}, &u}, nil
}

func terminateProcess(pid, exitcode int) error {
	h, e := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if e != nil {
		return os.NewSyscallError("OpenProcess", e)
	}
	defer func() {
		_ = windows.CloseHandle(h)
	}()
	e = windows.TerminateProcess(h, uint32(exitcode))
	return os.NewSyscallError("TerminateProcess", e)
}

func (p *Process) signal(sig os.Signal) error {
	handle := atomic.LoadUintptr(&p.handle)
	if handle == uintptr(windows.InvalidHandle) {
		return syscall.EINVAL
	}
	if p.done() {
		return errors.New("os: process already finished")
	}
	if sig == os.Kill {
		err := terminateProcess(p.Pid, 1)
		runtime.KeepAlive(p)
		return err
	}
	// TODO(rsc): Handle Interrupt too?
	return syscall.Errno(syscall.EWINDOWS)
}

func (p *Process) release() error {
	handle := atomic.LoadUintptr(&p.handle)
	if handle == uintptr(syscall.InvalidHandle) {
		return syscall.EINVAL
	}
	e := syscall.CloseHandle(syscall.Handle(handle))
	if e != nil {
		return os.NewSyscallError("CloseHandle", e)
	}
	atomic.StoreUintptr(&p.handle, uintptr(syscall.InvalidHandle))
	// no need for a finalizer anymore
	runtime.SetFinalizer(p, nil)
	return nil
}

func (p *Process) setDone() {
	atomic.StoreUint32(&p.isdone, 1)
}

func (p *Process) done() bool {
	return atomic.LoadUint32(&p.isdone) > 0
}

func init() {
	p := windows.GetCommandLine()
	cmd := windows.UTF16ToString((*[0xffff]uint16)(unsafe.Pointer(p))[:])
	if len(cmd) == 0 {
		arg0, _ := os.Executable()
		Args = []string{arg0}
	} else {
		Args = commandLineToArgv(cmd)
	}
}

// appendBSBytes appends n '\\' bytes to b and returns the resulting slice.
func appendBSBytes(b []byte, n int) []byte {
	for ; n > 0; n-- {
		b = append(b, '\\')
	}
	return b
}

// readNextArg splits command line string cmd into next
// argument and command line remainder.
func readNextArg(cmd string) (arg []byte, rest string) {
	var b []byte
	var inquote bool
	var nslash int
	for ; len(cmd) > 0; cmd = cmd[1:] {
		c := cmd[0]
		switch c {
		case ' ', '\t':
			if !inquote {
				return appendBSBytes(b, nslash), cmd[1:]
			}
		case '"':
			b = appendBSBytes(b, nslash/2)
			if nslash%2 == 0 {
				// use "Prior to 2008" rule from
				// http://daviddeley.com/autohotkey/parameters/parameters.htm
				// section 5.2 to deal with double double quotes
				if inquote && len(cmd) > 1 && cmd[1] == '"' {
					b = append(b, c)
					cmd = cmd[1:]
				}
				inquote = !inquote
			} else {
				b = append(b, c)
			}
			nslash = 0
			continue
		case '\\':
			nslash++
			continue
		}
		b = appendBSBytes(b, nslash)
		nslash = 0
		b = append(b, c)
	}
	return appendBSBytes(b, nslash), ""
}

// commandLineToArgv splits a command line into individual argument
// strings, following the Windows conventions documented
// at http://daviddeley.com/autohotkey/parameters/parameters.htm#WINARGV
func commandLineToArgv(cmd string) []string {
	var args []string
	for len(cmd) > 0 {
		if cmd[0] == ' ' || cmd[0] == '\t' {
			cmd = cmd[1:]
			continue
		}
		var arg []byte
		arg, cmd = readNextArg(cmd)
		args = append(args, string(arg))
	}
	return args
}

func itoa(val int) string { // do it here rather than with fmt to avoid dependency
	if val < 0 {
		return "-" + itoa(-val)
	}
	var buf [32]byte // big enough for int64
	i := len(buf) - 1
	for val >= 10 {
		buf[i] = byte(val%10 + '0')
		i--
		val /= 10
	}
	buf[i] = byte(val + '0')
	return string(buf[i:])
}

func ftToDuration(ft *windows.Filetime) time.Duration {
	n := int64(ft.HighDateTime)<<32 + int64(ft.LowDateTime) // in 100-nanosecond intervals
	return time.Duration(n*100) * time.Nanosecond
}

// ProcessState stores information about a process, as reported by Wait.
type ProcessState struct {
	pid    int                // The process's id.
	status windows.WaitStatus // System-dependent status info.
	rusage *windows.Rusage
}

// UserTime returns the user CPU time of the exited process and its children.
func (p *ProcessState) UserTime() time.Duration {
	return p.userTime()
}

// SystemTime returns the system CPU time of the exited process and its children.
func (p *ProcessState) SystemTime() time.Duration {
	return p.systemTime()
}

// Exited reports whether the program has exited.
func (p *ProcessState) Exited() bool {
	return p.exited()
}

// Success reports whether the program exited successfully,
// such as with exit status 0 on Unix.
func (p *ProcessState) Success() bool {
	return p.success()
}

// Sys returns system-dependent exit information about
// the process. Convert it to the appropriate underlying
// type, such as syscall.WaitStatus on Unix, to access its contents.
func (p *ProcessState) Sys() interface{} {
	return p.sys()
}

// SysUsage returns system-dependent resource usage information about
// the exited process. Convert it to the appropriate underlying
// type, such as *syscall.Rusage on Unix, to access its contents.
// (On Unix, *syscall.Rusage matches struct rusage as defined in the
// getrusage(2) manual page.)
func (p *ProcessState) SysUsage() interface{} {
	return p.sysUsage()
}

func (p *ProcessState) userTime() time.Duration {
	return ftToDuration(&p.rusage.UserTime)
}

func (p *ProcessState) systemTime() time.Duration {
	return ftToDuration(&p.rusage.KernelTime)
}

// Pid returns the process id of the exited process.
func (p *ProcessState) Pid() int {
	return p.pid
}

func (p *ProcessState) exited() bool {
	return p.status.Exited()
}

func (p *ProcessState) success() bool {
	return p.status.ExitStatus() == 0
}

func (p *ProcessState) sys() interface{} {
	return p.status
}

func (p *ProcessState) sysUsage() interface{} {
	return p.rusage
}

func (p *ProcessState) String() string {
	if p == nil {
		return "<nil>"
	}
	status := p.Sys().(syscall.WaitStatus)
	res := ""
	switch {
	case status.Exited():
		res = "exit status " + itoa(status.ExitStatus())
	case status.Signaled():
		res = "signal: " + status.Signal().String()
	case status.Stopped():
		res = "stop signal: " + status.StopSignal().String()
		if status.StopSignal() == syscall.SIGTRAP && status.TrapCause() != 0 {
			res += " (trap " + itoa(status.TrapCause()) + ")"
		}
	case status.Continued():
		res = "continued"
	}
	if status.CoreDump() {
		res += " (core dumped)"
	}
	return res
}

// ExitCode returns the exit code of the exited process, or -1
// if the process hasn't exited or was terminated by a signal.
func (p *ProcessState) ExitCode() int {
	// return -1 if the process hasn't started.
	if p == nil {
		return -1
	}
	return p.status.ExitStatus()
}

var ForkLock sync.RWMutex

// EscapeArg rewrites command line argument s as prescribed
// in https://msdn.microsoft.com/en-us/library/ms880421.
// This function returns "" (2 double quotes) if s is empty.
// Alternatively, these transformations are done:
// - every back slash (\) is doubled, but only if immediately
//   followed by double quote (");
// - every double quote (") is escaped by back slash (\);
// - finally, s is wrapped with double quotes (arg -> "arg"),
//   but only if there is space or tab inside s.
func EscapeArg(s string) string {
	if len(s) == 0 {
		return "\"\""
	}
	n := len(s)
	hasSpace := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\\':
			n++
		case ' ', '\t':
			hasSpace = true
		}
	}
	if hasSpace {
		n += 2
	}
	if n == len(s) {
		return s
	}

	qs := make([]byte, n)
	j := 0
	if hasSpace {
		qs[j] = '"'
		j++
	}
	slashes := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		default:
			slashes = 0
			qs[j] = s[i]
		case '\\':
			slashes++
			qs[j] = s[i]
		case '"':
			for ; slashes > 0; slashes-- {
				qs[j] = '\\'
				j++
			}
			qs[j] = '\\'
			j++
			qs[j] = s[i]
		}
		j++
	}
	if hasSpace {
		for ; slashes > 0; slashes-- {
			qs[j] = '\\'
			j++
		}
		qs[j] = '"'
		j++
	}
	return string(qs[:j])
}

// makeCmdLine builds a command line out of args by escaping "special"
// characters and joining the arguments with spaces.
func makeCmdLine(args []string) string {
	var s string
	for _, v := range args {
		if s != "" {
			s += " "
		}
		s += EscapeArg(v)
	}
	return s
}

// createEnvBlock converts an array of environment strings into
// the representation required by CreateProcess: a sequence of NUL
// terminated strings followed by a nil.
// Last bytes are two UCS-2 NULs, or four NUL bytes.
func createEnvBlock(envv []string) *uint16 {
	if len(envv) == 0 {
		return &utf16.Encode([]rune("\x00\x00"))[0]
	}
	length := 0
	for _, s := range envv {
		length += len(s) + 1
	}
	length += 1

	b := make([]byte, length)
	i := 0
	for _, s := range envv {
		l := len(s)
		copy(b[i:i+l], []byte(s))
		copy(b[i+l:i+l+1], []byte{0})
		i = i + l + 1
	}
	copy(b[i:i+1], []byte{0})

	return &utf16.Encode([]rune(string(b)))[0]
}

func CloseOnExec(fd windows.Handle) {
	_ = windows.SetHandleInformation(windows.Handle(fd), windows.HANDLE_FLAG_INHERIT, 0)
}

func SetNonblock(fd windows.Handle, nonblocking bool) (err error) {
	return nil
}

// FullPath retrieves the full path of the specified file.
func FullPath(name string) (path string, err error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return "", err
	}
	n := uint32(100)
	for {
		buf := make([]uint16, n)
		n, err = windows.GetFullPathName(p, uint32(len(buf)), &buf[0], nil)
		if err != nil {
			return "", err
		}
		if n <= uint32(len(buf)) {
			return windows.UTF16ToString(buf[:n]), nil
		}
	}
}

func isSlash(c uint8) bool {
	return c == '\\' || c == '/'
}

func normalizeDir(dir string) (name string, err error) {
	ndir, err := FullPath(dir)
	if err != nil {
		return "", err
	}
	if len(ndir) > 2 && isSlash(ndir[0]) && isSlash(ndir[1]) {
		// dir cannot have \\server\share\path form
		return "", syscall.EINVAL
	}
	return ndir, nil
}

func volToUpper(ch int) int {
	if 'a' <= ch && ch <= 'z' {
		ch += 'A' - 'a'
	}
	return ch
}

func joinExeDirAndFName(dir, p string) (name string, err error) {
	if len(p) == 0 {
		return "", syscall.EINVAL
	}
	if len(p) > 2 && isSlash(p[0]) && isSlash(p[1]) {
		// \\server\share\path form
		return p, nil
	}
	if len(p) > 1 && p[1] == ':' {
		// has drive letter
		if len(p) == 2 {
			return "", syscall.EINVAL
		}
		if isSlash(p[2]) {
			return p, nil
		} else {
			d, err := normalizeDir(dir)
			if err != nil {
				return "", err
			}
			if volToUpper(int(p[0])) == volToUpper(int(d[0])) {
				return FullPath(d + "\\" + p[2:])
			} else {
				return FullPath(p)
			}
		}
	} else {
		// no drive letter
		d, err := normalizeDir(dir)
		if err != nil {
			return "", err
		}
		if isSlash(p[0]) {
			return FullPath(d[:2] + p)
		} else {
			return FullPath(d + "\\" + p)
		}
	}
}
