package uvm

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"unsafe"

	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/requesttype"
	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/Microsoft/hcsshim/internal/winapi"
	"github.com/Microsoft/hcsshim/osversion"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
)

const (
	vsmbSharePrefix            = `\\?\VMSMB\VSMB-{dcc079ae-60ba-4d07-847c-3493609c0870}\`
	vsmbCurrentSerialVersionID = 1
)

// VSMBShare contains the host path for a Vsmb Mount
type VSMBShare struct {
	// UVM the resource belongs to
	vm              *UtilityVM
	HostPath        string
	refCount        uint32
	name            string
	allowedFiles    []string
	guestPath       string
	options         vm.VSMBOptions
	serialVersionID uint32
}

// Release frees the resources of the corresponding vsmb Mount
func (vsmb *VSMBShare) Release(ctx context.Context) error {
	if err := vsmb.vm.RemoveVSMB(ctx, vsmb.HostPath, vsmb.options.ReadOnly); err != nil {
		return fmt.Errorf("failed to remove VSMB share: %s", err)
	}
	return nil
}

// DefaultVSMBOptions returns the default VSMB options. If readOnly is specified,
// returns the default VSMB options for a readonly share.
func (uvm *UtilityVM) DefaultVSMBOptions(readOnly bool) *vm.VSMBOptions {
	opts := &vm.VSMBOptions{
		NoDirectMap: uvm.DevicesPhysicallyBacked() || uvm.VSMBNoDirectMap(),
	}
	if readOnly {
		opts.ShareRead = true
		opts.CacheIo = true
		opts.ReadOnly = true
		opts.PseudoOplocks = true
	}
	return opts
}

func (uvm *UtilityVM) SetSaveableVSMBOptions(opts *vm.VSMBOptions, readOnly bool) {
	if readOnly {
		opts.ShareRead = true
		opts.CacheIo = true
		opts.ReadOnly = true
		opts.PseudoOplocks = true
		opts.NoOplocks = false
	} else {
		// Using NoOpLocks can cause intermittent Access denied failures due to
		// a VSMB bug that was fixed but not backported to RS5/19H1.
		opts.ShareRead = false
		opts.CacheIo = false
		opts.ReadOnly = false
		opts.PseudoOplocks = false
		opts.NoOplocks = true
	}
	opts.NoLocks = true
	opts.PseudoDirnotify = true
	opts.NoDirectMap = true
}

// findVSMBShare finds a share by `hostPath`. If not found returns `ErrNotAttached`.
func (uvm *UtilityVM) findVSMBShare(ctx context.Context, m map[string]*VSMBShare, shareKey string) (*VSMBShare, error) {
	share, ok := m[shareKey]
	if !ok {
		return nil, ErrNotAttached
	}
	return share, nil
}

// openHostPath opens the given path and returns the handle. The handle is opened with
// full sharing and no access mask. The directory must already exist. This
// function is intended to return a handle suitable for use with GetFileInformationByHandleEx.
//
// We are not able to use builtin Go functionality for opening a directory path:
// - os.Open on a directory returns a os.File where Fd() is a search handle from FindFirstFile.
// - syscall.Open does not provide a way to specify FILE_FLAG_BACKUP_SEMANTICS, which is needed to
//   open a directory.
// We could use os.Open if the path is a file, but it's easier to just use the same code for both.
// Therefore, we call windows.CreateFile directly.
func openHostPath(path string) (windows.Handle, error) {
	u16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	h, err := windows.CreateFile(
		u16,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS,
		0)
	if err != nil {
		return 0, &os.PathError{
			Op:   "CreateFile",
			Path: path,
			Err:  err,
		}
	}
	return h, nil
}

// In 19H1, a change was made to VSMB to require querying file ID for the files being shared in
// order to support direct map. This change was made to ensure correctness in cases where direct
// map is used with saving/restoring VMs.
//
// However, certain file systems (such as Azure Files SMB shares) don't support the FileIdInfo
// query that is used. Azure Files in particular fails with ERROR_INVALID_PARAMETER. This issue
// affects at least 19H1, 19H2, 20H1, and 20H2.
//
// To work around this, we attempt to query for FileIdInfo ourselves if on an affected build. If
// the query fails, we override the specified options to force no direct map to be used.
func forceNoDirectMap(path string) (bool, error) {
	if ver := osversion.Build(); ver < osversion.V19H1 || ver > osversion.V20H2 {
		return false, nil
	}
	h, err := openHostPath(path)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = windows.CloseHandle(h)
	}()
	var info winapi.FILE_ID_INFO
	// We check for any error, rather than just ERROR_INVALID_PARAMETER. It seems better to also
	// fall back if e.g. some other backing filesystem is used which returns a different error.
	if err := windows.GetFileInformationByHandleEx(h, winapi.FileIdInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return true, nil
	}
	return false, nil
}

// AddVSMB adds a VSMB share to a Windows utility VM. Each VSMB share is ref-counted and
// only added if it isn't already. This is used for read-only layers, mapped directories
// to a container, and for mapped pipes.
func (uvm *UtilityVM) AddVSMB(ctx context.Context, hostPath string, options *vm.VSMBOptions) (*VSMBShare, error) {
	if uvm.operatingSystem != "windows" {
		return nil, errNotSupported
	}

	uvm.m.Lock()
	defer uvm.m.Unlock()

	// Temporary support to allow single-file mapping. If `hostPath` is a
	// directory, map it without restriction. However, if it is a file, map the
	// directory containing the file, and use `AllowedFileList` to only allow
	// access to that file. If the directory has been mapped before for
	// single-file use, add the new file to the `AllowedFileList` and issue an
	// Update operation.
	st, err := os.Stat(hostPath)
	if err != nil {
		return nil, err
	}
	var file string
	m := uvm.vsmbDirShares
	if !st.IsDir() {
		m = uvm.vsmbFileShares
		file = hostPath
		hostPath = filepath.Dir(hostPath)
		options.RestrictFileAccess = true
		options.SingleFileMapping = true
	}
	hostPath = filepath.Clean(hostPath)

	if force, err := forceNoDirectMap(hostPath); err != nil {
		return nil, err
	} else if force {
		log.G(ctx).WithField("path", hostPath).Info("Forcing NoDirectmap for VSMB mount")
		options.NoDirectMap = true
	}

	var requestType = requesttype.Update
	shareKey := getVSMBShareKey(hostPath, options.ReadOnly)
	share, err := uvm.findVSMBShare(ctx, m, shareKey)
	if err == ErrNotAttached {
		requestType = requesttype.Add
		uvm.vsmbCounter++
		shareName := "s" + strconv.FormatUint(uvm.vsmbCounter, 16)

		share = &VSMBShare{
			vm:              uvm,
			name:            shareName,
			guestPath:       vsmbSharePrefix + shareName,
			HostPath:        hostPath,
			serialVersionID: vsmbCurrentSerialVersionID,
		}
	}
	newAllowedFiles := share.allowedFiles
	if options.RestrictFileAccess {
		newAllowedFiles = append(newAllowedFiles, file)
	}

	// Update on a VSMB share currently only supports updating the
	// AllowedFileList, and in fact will return an error if RestrictFileAccess
	// isn't set (e.g. if used on an unrestricted share). So we only call Modify
	// if we are either doing an Add, or if RestrictFileAccess is set.
	if requestType == requesttype.Add || options.RestrictFileAccess {
		log.G(ctx).WithFields(logrus.Fields{
			"name":      share.name,
			"path":      hostPath,
			"options":   fmt.Sprintf("%+#v", options),
			"operation": requestType,
		}).Info("Modifying VSMB share")

		vsmb, ok := uvm.vm.(vm.VSMBManager)
		if !ok || !uvm.vm.Supported(vm.VSMB, vm.Add) {
			return nil, errors.Wrap(vm.ErrNotSupported, "stopping vsmb share add")
		}
		if err := vsmb.AddVSMB(ctx, hostPath, share.name, newAllowedFiles, options); err != nil {
			return nil, errors.Wrap(err, "failed to add vsmb share")
		}
	}

	share.allowedFiles = newAllowedFiles
	share.refCount++
	share.options = *options
	m[shareKey] = share
	return share, nil
}

// RemoveVSMB removes a VSMB share from a utility VM. Each VSMB share is ref-counted
// and only actually removed when the ref-count drops to zero.
func (uvm *UtilityVM) RemoveVSMB(ctx context.Context, hostPath string, readOnly bool) error {
	if uvm.operatingSystem != "windows" {
		return errNotSupported
	}

	uvm.m.Lock()
	defer uvm.m.Unlock()

	st, err := os.Stat(hostPath)
	if err != nil {
		return err
	}
	m := uvm.vsmbDirShares
	if !st.IsDir() {
		m = uvm.vsmbFileShares
		hostPath = filepath.Dir(hostPath)
	}
	hostPath = filepath.Clean(hostPath)
	shareKey := getVSMBShareKey(hostPath, readOnly)
	share, err := uvm.findVSMBShare(ctx, m, shareKey)
	if err != nil {
		return fmt.Errorf("%s is not present as a VSMB share in %s, cannot remove", hostPath, uvm.id)
	}

	share.refCount--
	if share.refCount > 0 {
		return nil
	}

	vsmb, ok := uvm.vm.(vm.VSMBManager)
	if !ok || !uvm.vm.Supported(vm.VSMB, vm.Remove) {
		return errors.Wrap(vm.ErrNotSupported, "stopping vsmb share removal")
	}
	if err := vsmb.RemoveVSMB(ctx, share.name); err != nil {
		return errors.Wrapf(err, "failed to remove vsmb share %s from %s", hostPath, uvm.id)
	}
	delete(m, shareKey)
	return nil
}

// GetVSMBUvmPath returns the guest path of a VSMB mount.
func (uvm *UtilityVM) GetVSMBUvmPath(ctx context.Context, hostPath string, readOnly bool) (string, error) {
	if hostPath == "" {
		return "", fmt.Errorf("no hostPath passed to GetVSMBUvmPath")
	}

	uvm.m.Lock()
	defer uvm.m.Unlock()

	st, err := os.Stat(hostPath)
	if err != nil {
		return "", err
	}
	m := uvm.vsmbDirShares
	f := ""
	if !st.IsDir() {
		m = uvm.vsmbFileShares
		hostPath, f = filepath.Split(hostPath)
	}
	hostPath = filepath.Clean(hostPath)
	shareKey := getVSMBShareKey(hostPath, readOnly)
	share, err := uvm.findVSMBShare(ctx, m, shareKey)
	if err != nil {
		return "", err
	}
	return filepath.Join(share.guestPath, f), nil
}

var _ = (Cloneable)(&VSMBShare{})

// GobEncode serializes the VSMBShare struct
func (vsmb *VSMBShare) GobEncode() ([]byte, error) {
	var buf bytes.Buffer
	encoder := gob.NewEncoder(&buf)
	errMsgFmt := "failed to encode VSMBShare: %s"
	// encode only the fields that can be safely deserialized.
	if err := encoder.Encode(vsmb.serialVersionID); err != nil {
		return nil, fmt.Errorf(errMsgFmt, err)
	}
	if err := encoder.Encode(vsmb.HostPath); err != nil {
		return nil, fmt.Errorf(errMsgFmt, err)
	}
	if err := encoder.Encode(vsmb.name); err != nil {
		return nil, fmt.Errorf(errMsgFmt, err)
	}
	if err := encoder.Encode(vsmb.allowedFiles); err != nil {
		return nil, fmt.Errorf(errMsgFmt, err)
	}
	if err := encoder.Encode(vsmb.guestPath); err != nil {
		return nil, fmt.Errorf(errMsgFmt, err)
	}
	if err := encoder.Encode(vsmb.options); err != nil {
		return nil, fmt.Errorf(errMsgFmt, err)
	}
	return buf.Bytes(), nil
}

// GobDecode deserializes the VSMBShare struct into the struct on which this is called
// (i.e the vsmb pointer)
func (vsmb *VSMBShare) GobDecode(data []byte) error {
	buf := bytes.NewBuffer(data)
	decoder := gob.NewDecoder(buf)
	errMsgFmt := "failed to decode VSMBShare: %s"
	// fields should be decoded in the same order in which they were encoded.
	// And verify the serialVersionID first
	if err := decoder.Decode(&vsmb.serialVersionID); err != nil {
		return fmt.Errorf(errMsgFmt, err)
	}
	if vsmb.serialVersionID != vsmbCurrentSerialVersionID {
		return fmt.Errorf("Serialized version of VSMBShare %d doesn't match with the current version %d", vsmb.serialVersionID, vsmbCurrentSerialVersionID)
	}
	if err := decoder.Decode(&vsmb.HostPath); err != nil {
		return fmt.Errorf(errMsgFmt, err)
	}
	if err := decoder.Decode(&vsmb.name); err != nil {
		return fmt.Errorf(errMsgFmt, err)
	}
	if err := decoder.Decode(&vsmb.allowedFiles); err != nil {
		return fmt.Errorf(errMsgFmt, err)
	}
	if err := decoder.Decode(&vsmb.guestPath); err != nil {
		return fmt.Errorf(errMsgFmt, err)
	}
	if err := decoder.Decode(&vsmb.options); err != nil {
		return fmt.Errorf(errMsgFmt, err)
	}
	return nil
}

// Clone creates a clone of the VSMBShare `vsmb` and adds that clone to the uvm `vm`. To
// clone VSMB share we just need to add it into the config doc of that VM and increase the
// vsmb counter.
func (vsmb *VSMBShare) Clone(ctx context.Context, uvm *UtilityVM, cd *cloneData) error {
	vsmbMgr, ok := cd.builder.(vm.VSMBManager)
	if !ok {
		return errors.Wrap(vm.ErrNotSupported, "stopping vsmb share operation")
	}
	if err := vsmbMgr.AddVSMB(ctx, vsmb.HostPath, vsmb.name, vsmb.allowedFiles, &vsmb.options); err != nil {
		return err
	}
	uvm.vsmbCounter++

	clonedVSMB := &VSMBShare{
		vm:              uvm,
		HostPath:        vsmb.HostPath,
		refCount:        1,
		name:            vsmb.name,
		options:         vsmb.options,
		allowedFiles:    vsmb.allowedFiles,
		guestPath:       vsmb.guestPath,
		serialVersionID: vsmbCurrentSerialVersionID,
	}

	if vsmb.options.RestrictFileAccess {
		uvm.vsmbFileShares[vsmb.HostPath] = clonedVSMB
	} else {
		uvm.vsmbDirShares[vsmb.HostPath] = clonedVSMB
	}
	return nil
}

// getVSMBShareKey returns a string key which encapsulates the information that is used to
// look up an existing VSMB share. If a share is being added, but there is an existing
// share with the same key, the existing share will be used instead (and its ref count
// incremented).
func getVSMBShareKey(hostPath string, readOnly bool) string {
	return fmt.Sprintf("%v-%v", hostPath, readOnly)
}

func (vsmb *VSMBShare) GetSerialVersionID() uint32 {
	return vsmbCurrentSerialVersionID
}
