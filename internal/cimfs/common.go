package cimfs

import (
	"time"

	"golang.org/x/sys/windows"
)

var (
	// Equivalent to SDDL of "D:NO_ACCESS_CONTROL"
	nullSd = []byte{1, 0, 4, 128, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
)

// 100ns units between Windows NT epoch (Jan 1 1601) and Unix epoch (Jan 1 1970)
const epochDelta = 116444736000000000

// Filetime is a Windows FILETIME, in 100-ns units since January 1, 1601.
type Filetime int64

// Time returns a Go time equivalent to `ft`.
func (ft Filetime) Time() time.Time {
	if ft == 0 {
		return time.Time{}
	}
	return time.Unix(0, (int64(ft)-epochDelta)*100)
}

func FiletimeFromTime(t time.Time) Filetime {
	if t.IsZero() {
		return 0
	}
	return Filetime(t.UnixNano()/100 + epochDelta)
}

func (ft Filetime) String() string {
	return ft.Time().String()
}

func (ft Filetime) toWindowsFiletime() windows.Filetime {
	return windows.NsecToFiletime((int64(ft) - epochDelta) * 100)
}

// FileInfo specifies information about a file.
type FileInfo struct {
	FileID             uint64 // ignored on write
	Size               int64
	Attributes         uint32
	CreationTime       Filetime
	LastWriteTime      Filetime
	ChangeTime         Filetime
	LastAccessTime     Filetime
	SecurityDescriptor []byte
	ExtendedAttributes []byte
	ReparseData        []byte
}

type OpError struct {
	Cim string
	Op  string
	Err error
}

func (e *OpError) Error() string {
	s := "cim " + e.Op + " " + e.Cim
	s += ": " + e.Err.Error()
	return s
}

// PathError is the error type returned by most functions in this package.
type PathError struct {
	Cim  string
	Op   string
	Path string
	Err  error
}

func (e *PathError) Error() string {
	s := "cim " + e.Op + " " + e.Cim
	s += ":" + e.Path
	s += ": " + e.Err.Error()
	return s
}

type StreamError struct {
	Cim    string
	Op     string
	Path   string
	Stream string
	Err    error
}

func (e *StreamError) Error() string {
	s := "cim " + e.Op + " " + e.Cim
	s += ":" + e.Path
	s += ":" + e.Stream
	s += ": " + e.Err.Error()
	return s
}

type LinkError struct {
	Cim string
	Op  string
	Old string
	New string
	Err error
}

func (e *LinkError) Error() string {
	return "cim " + e.Op + " " + e.Old + " " + e.New + ": " + e.Err.Error()
}
