package cimfs

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/Microsoft/go-winio/pkg/guid"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/winapi"
	"github.com/pkg/errors"
)

type MountError struct {
	Cim        string
	Op         string
	VolumeGUID guid.GUID
	Err        error
}

func (e *MountError) Error() string {
	s := "cim " + e.Op
	if e.Cim != "" {
		s += " " + e.Cim
	}
	s += " " + e.VolumeGUID.String() + ": " + e.Err.Error()
	return s
}

type cimInfo struct {
	// path to the cim
	path string
	// Unique GUID assigned to a cim.
	cimID guid.GUID
	// ref count for number of times this cim was mounted.
	refCount uint32
}

var mountMapLock sync.Mutex

// map for information about cims mounted on the host
var hostCimMounts = make(map[string]*cimInfo)

func MountWithFlags(cimPath string, mountFlags uint32) (string, error) {
	mountMapLock.Lock()
	defer mountMapLock.Unlock()
	if _, ok := hostCimMounts[cimPath]; !ok {
		layerGUID, err := guid.NewV4()
		if err != nil {
			return "", &MountError{Cim: cimPath, Op: "Mount", Err: err}
		}
		if err := winapi.CimMountImage(filepath.Dir(cimPath), filepath.Base(cimPath), mountFlags, &layerGUID); err != nil {
			return "", &MountError{Cim: cimPath, Op: "Mount", VolumeGUID: layerGUID, Err: err}
		}
		hostCimMounts[cimPath] = &cimInfo{cimPath, layerGUID, 0}
	}
	ci := hostCimMounts[cimPath]
	ci.refCount += 1
	return fmt.Sprintf("\\\\?\\Volume{%s}\\", ci.cimID), nil
}

// Mount mounts the cim at path `cimPath` and returns the mount location of that cim.
// If this cim is already mounted then nothing is done.
// This method uses the `CimMountFlagCacheRegions` mount flag when mounting the cim, if some other
// mount flag is desired use the `MountWithFlags` method.
func Mount(cimPath string) (string, error) {
	return MountWithFlags(cimPath, hcsschema.CimMountFlagCacheRegions)
}

// Returns the path ("\\?\Volume{GUID}" format) at which the cim with given cimPath is mounted
// Throws an error if the given cim is not mounted.
func GetCimMountPath(cimPath string) (string, error) {
	mountMapLock.Lock()
	defer mountMapLock.Unlock()
	ci, ok := hostCimMounts[cimPath]
	if !ok {
		return "", errors.Errorf("cim %s is not mounted", cimPath)
	}
	return fmt.Sprintf("\\\\?\\Volume{%s}\\", ci.cimID), nil
}

// Unmount unmounts the cim at path `cimPath` if this is the last reference to it.
func Unmount(cimPath string) error {
	mountMapLock.Lock()
	defer mountMapLock.Unlock()
	ci, ok := hostCimMounts[cimPath]
	if !ok {
		return errors.Errorf("cim not mounted")
	}
	if ci.refCount == 1 {
		if err := winapi.CimDismountImage(&ci.cimID); err != nil {
			return &MountError{Cim: cimPath, Op: "Unmount", Err: err}
		}
		delete(hostCimMounts, cimPath)
	} else {
		ci.refCount -= 1
	}
	return nil
}
