// Code generated mksyscall_windows.exe DO NOT EDIT

package winapi

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var _ unsafe.Pointer

// Do the interface allocations only once for common
// Errno values.
const (
	errnoERROR_IO_PENDING = 997
)

var (
	errERROR_IO_PENDING error = syscall.Errno(errnoERROR_IO_PENDING)
)

// errnoErr returns common boxed Errno values, to prevent
// allocations at runtime.
func errnoErr(e syscall.Errno) error {
	switch e {
	case 0:
		return nil
	case errnoERROR_IO_PENDING:
		return errERROR_IO_PENDING
	}
	// TODO: add more here, after collecting data on the common
	// error values see on Windows. (perhaps when running
	// all.bat?)
	return e
}

var (
	modntdll    = windows.NewLazySystemDLL("ntdll.dll")
	modiphlpapi = windows.NewLazySystemDLL("iphlpapi.dll")
	modkernel32 = windows.NewLazySystemDLL("kernel32.dll")
	modadvapi32 = windows.NewLazySystemDLL("advapi32.dll")
	modpsapi    = windows.NewLazySystemDLL("psapi.dll")
	modcfgmgr32 = windows.NewLazySystemDLL("cfgmgr32.dll")
	modoffreg   = windows.NewLazySystemDLL("offreg.dll")
	modcimfs    = windows.NewLazySystemDLL("cimfs.dll")

	procNtQuerySystemInformation               = modntdll.NewProc("NtQuerySystemInformation")
	procSetJobCompartmentId                    = modiphlpapi.NewProc("SetJobCompartmentId")
	procSearchPathW                            = modkernel32.NewProc("SearchPathW")
	procCreateRemoteThread                     = modkernel32.NewProc("CreateRemoteThread")
	procGetQueuedCompletionStatus              = modkernel32.NewProc("GetQueuedCompletionStatus")
	procIsProcessInJob                         = modkernel32.NewProc("IsProcessInJob")
	procQueryInformationJobObject              = modkernel32.NewProc("QueryInformationJobObject")
	procOpenJobObjectW                         = modkernel32.NewProc("OpenJobObjectW")
	procSetIoRateControlInformationJobObject   = modkernel32.NewProc("SetIoRateControlInformationJobObject")
	procQueryIoRateControlInformationJobObject = modkernel32.NewProc("QueryIoRateControlInformationJobObject")
	procNtOpenJobObject                        = modntdll.NewProc("NtOpenJobObject")
	procNtCreateJobObject                      = modntdll.NewProc("NtCreateJobObject")
	procLogonUserW                             = modadvapi32.NewProc("LogonUserW")
	procRtlMoveMemory                          = modkernel32.NewProc("RtlMoveMemory")
	procLocalAlloc                             = modkernel32.NewProc("LocalAlloc")
	procLocalFree                              = modkernel32.NewProc("LocalFree")
	procQueryWorkingSet                        = modpsapi.NewProc("QueryWorkingSet")
	procGetProcessImageFileNameW               = modkernel32.NewProc("GetProcessImageFileNameW")
	procGetActiveProcessorCount                = modkernel32.NewProc("GetActiveProcessorCount")
	procCM_Get_Device_ID_List_SizeA            = modcfgmgr32.NewProc("CM_Get_Device_ID_List_SizeA")
	procCM_Get_Device_ID_ListA                 = modcfgmgr32.NewProc("CM_Get_Device_ID_ListA")
	procCM_Locate_DevNodeW                     = modcfgmgr32.NewProc("CM_Locate_DevNodeW")
	procCM_Get_DevNode_PropertyW               = modcfgmgr32.NewProc("CM_Get_DevNode_PropertyW")
	procNtCreateFile                           = modntdll.NewProc("NtCreateFile")
	procNtSetInformationFile                   = modntdll.NewProc("NtSetInformationFile")
	procNtOpenDirectoryObject                  = modntdll.NewProc("NtOpenDirectoryObject")
	procNtQueryDirectoryObject                 = modntdll.NewProc("NtQueryDirectoryObject")
	procRtlNtStatusToDosError                  = modntdll.NewProc("RtlNtStatusToDosError")
	procORMergeHives                           = modoffreg.NewProc("ORMergeHives")
	procOROpenHive                             = modoffreg.NewProc("OROpenHive")
	procORCloseHive                            = modoffreg.NewProc("ORCloseHive")
	procORSaveHive                             = modoffreg.NewProc("ORSaveHive")
	procOROpenKey                              = modoffreg.NewProc("OROpenKey")
	procORCloseKey                             = modoffreg.NewProc("ORCloseKey")
	procORCreateKey                            = modoffreg.NewProc("ORCreateKey")
	procORDeleteKey                            = modoffreg.NewProc("ORDeleteKey")
	procORGetValue                             = modoffreg.NewProc("ORGetValue")
	procORSetValue                             = modoffreg.NewProc("ORSetValue")
	procCimMountImage                          = modcimfs.NewProc("CimMountImage")
	procCimDismountImage                       = modcimfs.NewProc("CimDismountImage")
	procCimCreateImage                         = modcimfs.NewProc("CimCreateImage")
	procCimCloseImage                          = modcimfs.NewProc("CimCloseImage")
	procCimCommitImage                         = modcimfs.NewProc("CimCommitImage")
	procCimCreateFile                          = modcimfs.NewProc("CimCreateFile")
	procCimCloseStream                         = modcimfs.NewProc("CimCloseStream")
	procCimWriteStream                         = modcimfs.NewProc("CimWriteStream")
	procCimDeletePath                          = modcimfs.NewProc("CimDeletePath")
	procCimCreateHardLink                      = modcimfs.NewProc("CimCreateHardLink")
	procCimCreateAlternateStream               = modcimfs.NewProc("CimCreateAlternateStream")
)

func NtQuerySystemInformation(systemInfoClass int, systemInformation uintptr, systemInfoLength uint32, returnLength *uint32) (status uint32) {
	r0, _, _ := syscall.Syscall6(procNtQuerySystemInformation.Addr(), 4, uintptr(systemInfoClass), uintptr(systemInformation), uintptr(systemInfoLength), uintptr(unsafe.Pointer(returnLength)), 0, 0)
	status = uint32(r0)
	return
}

func SetJobCompartmentId(handle windows.Handle, compartmentId uint32) (win32Err error) {
	r0, _, _ := syscall.Syscall(procSetJobCompartmentId.Addr(), 2, uintptr(handle), uintptr(compartmentId), 0)
	if r0 != 0 {
		win32Err = syscall.Errno(r0)
	}
	return
}

func SearchPath(lpPath *uint16, lpFileName *uint16, lpExtension *uint16, nBufferLength uint32, lpBuffer *uint16, lpFilePath *uint16) (size uint32, err error) {
	r0, _, e1 := syscall.Syscall6(procSearchPathW.Addr(), 6, uintptr(unsafe.Pointer(lpPath)), uintptr(unsafe.Pointer(lpFileName)), uintptr(unsafe.Pointer(lpExtension)), uintptr(nBufferLength), uintptr(unsafe.Pointer(lpBuffer)), uintptr(unsafe.Pointer(lpFilePath)))
	size = uint32(r0)
	if size == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func CreateRemoteThread(process windows.Handle, sa *windows.SecurityAttributes, stackSize uint32, startAddr uintptr, parameter uintptr, creationFlags uint32, threadID *uint32) (handle windows.Handle, err error) {
	r0, _, e1 := syscall.Syscall9(procCreateRemoteThread.Addr(), 7, uintptr(process), uintptr(unsafe.Pointer(sa)), uintptr(stackSize), uintptr(startAddr), uintptr(parameter), uintptr(creationFlags), uintptr(unsafe.Pointer(threadID)), 0, 0)
	handle = windows.Handle(r0)
	if handle == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func GetQueuedCompletionStatus(cphandle windows.Handle, qty *uint32, key *uintptr, overlapped **windows.Overlapped, timeout uint32) (err error) {
	r1, _, e1 := syscall.Syscall6(procGetQueuedCompletionStatus.Addr(), 5, uintptr(cphandle), uintptr(unsafe.Pointer(qty)), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(overlapped)), uintptr(timeout), 0)
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func IsProcessInJob(procHandle windows.Handle, jobHandle windows.Handle, result *bool) (err error) {
	r1, _, e1 := syscall.Syscall(procIsProcessInJob.Addr(), 3, uintptr(procHandle), uintptr(jobHandle), uintptr(unsafe.Pointer(result)))
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func QueryInformationJobObject(jobHandle windows.Handle, infoClass uint32, jobObjectInfo uintptr, jobObjectInformationLength uint32, lpReturnLength *uint32) (err error) {
	r1, _, e1 := syscall.Syscall6(procQueryInformationJobObject.Addr(), 5, uintptr(jobHandle), uintptr(infoClass), uintptr(jobObjectInfo), uintptr(jobObjectInformationLength), uintptr(unsafe.Pointer(lpReturnLength)), 0)
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func OpenJobObject(desiredAccess uint32, inheritHandle bool, lpName *uint16) (handle windows.Handle, err error) {
	var _p0 uint32
	if inheritHandle {
		_p0 = 1
	} else {
		_p0 = 0
	}
	r0, _, e1 := syscall.Syscall(procOpenJobObjectW.Addr(), 3, uintptr(desiredAccess), uintptr(_p0), uintptr(unsafe.Pointer(lpName)))
	handle = windows.Handle(r0)
	if handle == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func SetIoRateControlInformationJobObject(jobHandle windows.Handle, ioRateControlInfo *JOBOBJECT_IO_RATE_CONTROL_INFORMATION) (ret uint32, err error) {
	r0, _, e1 := syscall.Syscall(procSetIoRateControlInformationJobObject.Addr(), 2, uintptr(jobHandle), uintptr(unsafe.Pointer(ioRateControlInfo)), 0)
	ret = uint32(r0)
	if ret == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func QueryIoRateControlInformationJobObject(jobHandle windows.Handle, volumeName *uint16, ioRateControlInfo **JOBOBJECT_IO_RATE_CONTROL_INFORMATION, infoBlockCount *uint32) (ret uint32, err error) {
	r0, _, e1 := syscall.Syscall6(procQueryIoRateControlInformationJobObject.Addr(), 4, uintptr(jobHandle), uintptr(unsafe.Pointer(volumeName)), uintptr(unsafe.Pointer(ioRateControlInfo)), uintptr(unsafe.Pointer(infoBlockCount)), 0, 0)
	ret = uint32(r0)
	if ret == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func NtOpenJobObject(jobHandle *windows.Handle, desiredAccess uint32, objAttributes *ObjectAttributes) (status uint32) {
	r0, _, _ := syscall.Syscall(procNtOpenJobObject.Addr(), 3, uintptr(unsafe.Pointer(jobHandle)), uintptr(desiredAccess), uintptr(unsafe.Pointer(objAttributes)))
	status = uint32(r0)
	return
}

func NtCreateJobObject(jobHandle *windows.Handle, desiredAccess uint32, objAttributes *ObjectAttributes) (status uint32) {
	r0, _, _ := syscall.Syscall(procNtCreateJobObject.Addr(), 3, uintptr(unsafe.Pointer(jobHandle)), uintptr(desiredAccess), uintptr(unsafe.Pointer(objAttributes)))
	status = uint32(r0)
	return
}

func LogonUser(username *uint16, domain *uint16, password *uint16, logonType uint32, logonProvider uint32, token *windows.Token) (err error) {
	r1, _, e1 := syscall.Syscall6(procLogonUserW.Addr(), 6, uintptr(unsafe.Pointer(username)), uintptr(unsafe.Pointer(domain)), uintptr(unsafe.Pointer(password)), uintptr(logonType), uintptr(logonProvider), uintptr(unsafe.Pointer(token)))
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func RtlMoveMemory(destination *byte, source *byte, length uintptr) (err error) {
	r1, _, e1 := syscall.Syscall(procRtlMoveMemory.Addr(), 3, uintptr(unsafe.Pointer(destination)), uintptr(unsafe.Pointer(source)), uintptr(length))
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func LocalAlloc(flags uint32, size int) (ptr uintptr) {
	r0, _, _ := syscall.Syscall(procLocalAlloc.Addr(), 2, uintptr(flags), uintptr(size), 0)
	ptr = uintptr(r0)
	return
}

func LocalFree(ptr uintptr) {
	syscall.Syscall(procLocalFree.Addr(), 1, uintptr(ptr), 0, 0)
	return
}

func QueryWorkingSet(handle windows.Handle, pv uintptr, cb uint32) (err error) {
	r1, _, e1 := syscall.Syscall(procQueryWorkingSet.Addr(), 3, uintptr(handle), uintptr(pv), uintptr(cb))
	if r1 == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func GetProcessImageFileName(hProcess windows.Handle, imageFileName *uint16, nSize uint32) (size uint32, err error) {
	r0, _, e1 := syscall.Syscall(procGetProcessImageFileNameW.Addr(), 3, uintptr(hProcess), uintptr(unsafe.Pointer(imageFileName)), uintptr(nSize))
	size = uint32(r0)
	if size == 0 {
		if e1 != 0 {
			err = errnoErr(e1)
		} else {
			err = syscall.EINVAL
		}
	}
	return
}

func GetActiveProcessorCount(groupNumber uint16) (amount uint32) {
	r0, _, _ := syscall.Syscall(procGetActiveProcessorCount.Addr(), 1, uintptr(groupNumber), 0, 0)
	amount = uint32(r0)
	return
}

func CMGetDeviceIDListSize(pulLen *uint32, pszFilter *byte, uFlags uint32) (hr error) {
	r0, _, _ := syscall.Syscall(procCM_Get_Device_ID_List_SizeA.Addr(), 3, uintptr(unsafe.Pointer(pulLen)), uintptr(unsafe.Pointer(pszFilter)), uintptr(uFlags))
	if int32(r0) < 0 {
		if r0&0x1fff0000 == 0x00070000 {
			r0 &= 0xffff
		}
		hr = syscall.Errno(r0)
	}
	return
}

func CMGetDeviceIDList(pszFilter *byte, buffer *byte, bufferLen uint32, uFlags uint32) (hr error) {
	r0, _, _ := syscall.Syscall6(procCM_Get_Device_ID_ListA.Addr(), 4, uintptr(unsafe.Pointer(pszFilter)), uintptr(unsafe.Pointer(buffer)), uintptr(bufferLen), uintptr(uFlags), 0, 0)
	if int32(r0) < 0 {
		if r0&0x1fff0000 == 0x00070000 {
			r0 &= 0xffff
		}
		hr = syscall.Errno(r0)
	}
	return
}

func CMLocateDevNode(pdnDevInst *uint32, pDeviceID string, uFlags uint32) (hr error) {
	var _p0 *uint16
	_p0, hr = syscall.UTF16PtrFromString(pDeviceID)
	if hr != nil {
		return
	}
	return _CMLocateDevNode(pdnDevInst, _p0, uFlags)
}

func _CMLocateDevNode(pdnDevInst *uint32, pDeviceID *uint16, uFlags uint32) (hr error) {
	r0, _, _ := syscall.Syscall(procCM_Locate_DevNodeW.Addr(), 3, uintptr(unsafe.Pointer(pdnDevInst)), uintptr(unsafe.Pointer(pDeviceID)), uintptr(uFlags))
	if int32(r0) < 0 {
		if r0&0x1fff0000 == 0x00070000 {
			r0 &= 0xffff
		}
		hr = syscall.Errno(r0)
	}
	return
}

func CMGetDevNodeProperty(dnDevInst uint32, propertyKey *DevPropKey, propertyType *uint32, propertyBuffer *uint16, propertyBufferSize *uint32, uFlags uint32) (hr error) {
	r0, _, _ := syscall.Syscall6(procCM_Get_DevNode_PropertyW.Addr(), 6, uintptr(dnDevInst), uintptr(unsafe.Pointer(propertyKey)), uintptr(unsafe.Pointer(propertyType)), uintptr(unsafe.Pointer(propertyBuffer)), uintptr(unsafe.Pointer(propertyBufferSize)), uintptr(uFlags))
	if int32(r0) < 0 {
		if r0&0x1fff0000 == 0x00070000 {
			r0 &= 0xffff
		}
		hr = syscall.Errno(r0)
	}
	return
}

func NtCreateFile(handle *uintptr, accessMask uint32, oa *ObjectAttributes, iosb *IOStatusBlock, allocationSize *uint64, fileAttributes uint32, shareAccess uint32, createDisposition uint32, createOptions uint32, eaBuffer *byte, eaLength uint32) (status uint32) {
	r0, _, _ := syscall.Syscall12(procNtCreateFile.Addr(), 11, uintptr(unsafe.Pointer(handle)), uintptr(accessMask), uintptr(unsafe.Pointer(oa)), uintptr(unsafe.Pointer(iosb)), uintptr(unsafe.Pointer(allocationSize)), uintptr(fileAttributes), uintptr(shareAccess), uintptr(createDisposition), uintptr(createOptions), uintptr(unsafe.Pointer(eaBuffer)), uintptr(eaLength), 0)
	status = uint32(r0)
	return
}

func NtSetInformationFile(handle uintptr, iosb *IOStatusBlock, information uintptr, length uint32, class uint32) (status uint32) {
	r0, _, _ := syscall.Syscall6(procNtSetInformationFile.Addr(), 5, uintptr(handle), uintptr(unsafe.Pointer(iosb)), uintptr(information), uintptr(length), uintptr(class), 0)
	status = uint32(r0)
	return
}

func NtOpenDirectoryObject(handle *uintptr, accessMask uint32, oa *ObjectAttributes) (status uint32) {
	r0, _, _ := syscall.Syscall(procNtOpenDirectoryObject.Addr(), 3, uintptr(unsafe.Pointer(handle)), uintptr(accessMask), uintptr(unsafe.Pointer(oa)))
	status = uint32(r0)
	return
}

func NtQueryDirectoryObject(handle uintptr, buffer *byte, length uint32, singleEntry bool, restartScan bool, context *uint32, returnLength *uint32) (status uint32) {
	var _p0 uint32
	if singleEntry {
		_p0 = 1
	} else {
		_p0 = 0
	}
	var _p1 uint32
	if restartScan {
		_p1 = 1
	} else {
		_p1 = 0
	}
	r0, _, _ := syscall.Syscall9(procNtQueryDirectoryObject.Addr(), 7, uintptr(handle), uintptr(unsafe.Pointer(buffer)), uintptr(length), uintptr(_p0), uintptr(_p1), uintptr(unsafe.Pointer(context)), uintptr(unsafe.Pointer(returnLength)), 0, 0)
	status = uint32(r0)
	return
}

func RtlNtStatusToDosError(status uint32) (winerr error) {
	r0, _, _ := syscall.Syscall(procRtlNtStatusToDosError.Addr(), 1, uintptr(status), 0, 0)
	if r0 != 0 {
		winerr = syscall.Errno(r0)
	}
	return
}

func OrMergeHives(hiveHandles []OrHKey, result *OrHKey) (win32err error) {
	var _p0 *OrHKey
	if len(hiveHandles) > 0 {
		_p0 = &hiveHandles[0]
	}
	r0, _, _ := syscall.Syscall(procORMergeHives.Addr(), 3, uintptr(unsafe.Pointer(_p0)), uintptr(len(hiveHandles)), uintptr(unsafe.Pointer(result)))
	if r0 != 0 {
		win32err = syscall.Errno(r0)
	}
	return
}

func OrOpenHive(hivePath string, result *OrHKey) (win32err error) {
	var _p0 *uint16
	_p0, win32err = syscall.UTF16PtrFromString(hivePath)
	if win32err != nil {
		return
	}
	return _OrOpenHive(_p0, result)
}

func _OrOpenHive(hivePath *uint16, result *OrHKey) (win32err error) {
	r0, _, _ := syscall.Syscall(procOROpenHive.Addr(), 2, uintptr(unsafe.Pointer(hivePath)), uintptr(unsafe.Pointer(result)), 0)
	if r0 != 0 {
		win32err = syscall.Errno(r0)
	}
	return
}

func OrCloseHive(handle OrHKey) (win32err error) {
	r0, _, _ := syscall.Syscall(procORCloseHive.Addr(), 1, uintptr(handle), 0, 0)
	if r0 != 0 {
		win32err = syscall.Errno(r0)
	}
	return
}

func OrSaveHive(handle OrHKey, hivePath string, osMajorVersion uint32, osMinorVersion uint32) (win32err error) {
	var _p0 *uint16
	_p0, win32err = syscall.UTF16PtrFromString(hivePath)
	if win32err != nil {
		return
	}
	return _OrSaveHive(handle, _p0, osMajorVersion, osMinorVersion)
}

func _OrSaveHive(handle OrHKey, hivePath *uint16, osMajorVersion uint32, osMinorVersion uint32) (win32err error) {
	r0, _, _ := syscall.Syscall6(procORSaveHive.Addr(), 4, uintptr(handle), uintptr(unsafe.Pointer(hivePath)), uintptr(osMajorVersion), uintptr(osMinorVersion), 0, 0)
	if r0 != 0 {
		win32err = syscall.Errno(r0)
	}
	return
}

func OrOpenKey(handle OrHKey, subKey string, result *OrHKey) (win32err error) {
	var _p0 *uint16
	_p0, win32err = syscall.UTF16PtrFromString(subKey)
	if win32err != nil {
		return
	}
	return _OrOpenKey(handle, _p0, result)
}

func _OrOpenKey(handle OrHKey, subKey *uint16, result *OrHKey) (win32err error) {
	r0, _, _ := syscall.Syscall(procOROpenKey.Addr(), 3, uintptr(handle), uintptr(unsafe.Pointer(subKey)), uintptr(unsafe.Pointer(result)))
	if r0 != 0 {
		win32err = syscall.Errno(r0)
	}
	return
}

func OrCloseKey(handle OrHKey) (win32err error) {
	r0, _, _ := syscall.Syscall(procORCloseKey.Addr(), 1, uintptr(handle), 0, 0)
	if r0 != 0 {
		win32err = syscall.Errno(r0)
	}
	return
}

func OrCreateKey(handle OrHKey, subKey string, class uintptr, options uint32, securityDescriptor uintptr, result *OrHKey, disposition *uint32) (win32err error) {
	var _p0 *uint16
	_p0, win32err = syscall.UTF16PtrFromString(subKey)
	if win32err != nil {
		return
	}
	return _OrCreateKey(handle, _p0, class, options, securityDescriptor, result, disposition)
}

func _OrCreateKey(handle OrHKey, subKey *uint16, class uintptr, options uint32, securityDescriptor uintptr, result *OrHKey, disposition *uint32) (win32err error) {
	r0, _, _ := syscall.Syscall9(procORCreateKey.Addr(), 7, uintptr(handle), uintptr(unsafe.Pointer(subKey)), uintptr(class), uintptr(options), uintptr(securityDescriptor), uintptr(unsafe.Pointer(result)), uintptr(unsafe.Pointer(disposition)), 0, 0)
	if r0 != 0 {
		win32err = syscall.Errno(r0)
	}
	return
}

func OrDeleteKey(handle OrHKey, subKey string) (win32err error) {
	var _p0 *uint16
	_p0, win32err = syscall.UTF16PtrFromString(subKey)
	if win32err != nil {
		return
	}
	return _OrDeleteKey(handle, _p0)
}

func _OrDeleteKey(handle OrHKey, subKey *uint16) (win32err error) {
	r0, _, _ := syscall.Syscall(procORDeleteKey.Addr(), 2, uintptr(handle), uintptr(unsafe.Pointer(subKey)), 0)
	if r0 != 0 {
		win32err = syscall.Errno(r0)
	}
	return
}

func OrGetValue(handle OrHKey, subKey string, value string, valueType *uint32, data *byte, dataLen *uint32) (win32err error) {
	var _p0 *uint16
	_p0, win32err = syscall.UTF16PtrFromString(subKey)
	if win32err != nil {
		return
	}
	var _p1 *uint16
	_p1, win32err = syscall.UTF16PtrFromString(value)
	if win32err != nil {
		return
	}
	return _OrGetValue(handle, _p0, _p1, valueType, data, dataLen)
}

func _OrGetValue(handle OrHKey, subKey *uint16, value *uint16, valueType *uint32, data *byte, dataLen *uint32) (win32err error) {
	r0, _, _ := syscall.Syscall6(procORGetValue.Addr(), 6, uintptr(handle), uintptr(unsafe.Pointer(subKey)), uintptr(unsafe.Pointer(value)), uintptr(unsafe.Pointer(valueType)), uintptr(unsafe.Pointer(data)), uintptr(unsafe.Pointer(dataLen)))
	if r0 != 0 {
		win32err = syscall.Errno(r0)
	}
	return
}

func OrSetValue(handle OrHKey, valueName string, valueType uint32, data *byte, dataLen uint32) (win32err error) {
	var _p0 *uint16
	_p0, win32err = syscall.UTF16PtrFromString(valueName)
	if win32err != nil {
		return
	}
	return _OrSetValue(handle, _p0, valueType, data, dataLen)
}

func _OrSetValue(handle OrHKey, valueName *uint16, valueType uint32, data *byte, dataLen uint32) (win32err error) {
	r0, _, _ := syscall.Syscall6(procORSetValue.Addr(), 5, uintptr(handle), uintptr(unsafe.Pointer(valueName)), uintptr(valueType), uintptr(unsafe.Pointer(data)), uintptr(dataLen), 0)
	if r0 != 0 {
		win32err = syscall.Errno(r0)
	}
	return
}

func CimMountImage(imagePath string, fsName string, flags uint32, volumeID *g) (hr error) {
	var _p0 *uint16
	_p0, hr = syscall.UTF16PtrFromString(imagePath)
	if hr != nil {
		return
	}
	var _p1 *uint16
	_p1, hr = syscall.UTF16PtrFromString(fsName)
	if hr != nil {
		return
	}
	return _CimMountImage(_p0, _p1, flags, volumeID)
}

func _CimMountImage(imagePath *uint16, fsName *uint16, flags uint32, volumeID *g) (hr error) {
	if hr = procCimMountImage.Find(); hr != nil {
		return
	}
	r0, _, _ := syscall.Syscall6(procCimMountImage.Addr(), 4, uintptr(unsafe.Pointer(imagePath)), uintptr(unsafe.Pointer(fsName)), uintptr(flags), uintptr(unsafe.Pointer(volumeID)), 0, 0)
	if int32(r0) < 0 {
		if r0&0x1fff0000 == 0x00070000 {
			r0 &= 0xffff
		}
		hr = syscall.Errno(r0)
	}
	return
}

func CimDismountImage(volumeID *g) (hr error) {
	if hr = procCimDismountImage.Find(); hr != nil {
		return
	}
	r0, _, _ := syscall.Syscall(procCimDismountImage.Addr(), 1, uintptr(unsafe.Pointer(volumeID)), 0, 0)
	if int32(r0) < 0 {
		if r0&0x1fff0000 == 0x00070000 {
			r0 &= 0xffff
		}
		hr = syscall.Errno(r0)
	}
	return
}

func CimCreateImage(imagePath string, oldFSName *uint16, newFSName *uint16, cimFSHandle *FsHandle) (hr error) {
	var _p0 *uint16
	_p0, hr = syscall.UTF16PtrFromString(imagePath)
	if hr != nil {
		return
	}
	return _CimCreateImage(_p0, oldFSName, newFSName, cimFSHandle)
}

func _CimCreateImage(imagePath *uint16, oldFSName *uint16, newFSName *uint16, cimFSHandle *FsHandle) (hr error) {
	if hr = procCimCreateImage.Find(); hr != nil {
		return
	}
	r0, _, _ := syscall.Syscall6(procCimCreateImage.Addr(), 4, uintptr(unsafe.Pointer(imagePath)), uintptr(unsafe.Pointer(oldFSName)), uintptr(unsafe.Pointer(newFSName)), uintptr(unsafe.Pointer(cimFSHandle)), 0, 0)
	if int32(r0) < 0 {
		if r0&0x1fff0000 == 0x00070000 {
			r0 &= 0xffff
		}
		hr = syscall.Errno(r0)
	}
	return
}

func CimCloseImage(cimFSHandle FsHandle) (hr error) {
	if hr = procCimCloseImage.Find(); hr != nil {
		return
	}
	r0, _, _ := syscall.Syscall(procCimCloseImage.Addr(), 1, uintptr(cimFSHandle), 0, 0)
	if int32(r0) < 0 {
		if r0&0x1fff0000 == 0x00070000 {
			r0 &= 0xffff
		}
		hr = syscall.Errno(r0)
	}
	return
}

func CimCommitImage(cimFSHandle FsHandle) (hr error) {
	if hr = procCimCommitImage.Find(); hr != nil {
		return
	}
	r0, _, _ := syscall.Syscall(procCimCommitImage.Addr(), 1, uintptr(cimFSHandle), 0, 0)
	if int32(r0) < 0 {
		if r0&0x1fff0000 == 0x00070000 {
			r0 &= 0xffff
		}
		hr = syscall.Errno(r0)
	}
	return
}

func CimCreateFile(cimFSHandle FsHandle, path string, file *CimFsFileMetadata, cimStreamHandle *StreamHandle) (hr error) {
	var _p0 *uint16
	_p0, hr = syscall.UTF16PtrFromString(path)
	if hr != nil {
		return
	}
	return _CimCreateFile(cimFSHandle, _p0, file, cimStreamHandle)
}

func _CimCreateFile(cimFSHandle FsHandle, path *uint16, file *CimFsFileMetadata, cimStreamHandle *StreamHandle) (hr error) {
	if hr = procCimCreateFile.Find(); hr != nil {
		return
	}
	r0, _, _ := syscall.Syscall6(procCimCreateFile.Addr(), 4, uintptr(cimFSHandle), uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(file)), uintptr(unsafe.Pointer(cimStreamHandle)), 0, 0)
	if int32(r0) < 0 {
		if r0&0x1fff0000 == 0x00070000 {
			r0 &= 0xffff
		}
		hr = syscall.Errno(r0)
	}
	return
}

func CimCloseStream(cimStreamHandle StreamHandle) (hr error) {
	if hr = procCimCloseStream.Find(); hr != nil {
		return
	}
	r0, _, _ := syscall.Syscall(procCimCloseStream.Addr(), 1, uintptr(cimStreamHandle), 0, 0)
	if int32(r0) < 0 {
		if r0&0x1fff0000 == 0x00070000 {
			r0 &= 0xffff
		}
		hr = syscall.Errno(r0)
	}
	return
}

func CimWriteStream(cimStreamHandle StreamHandle, buffer uintptr, bufferSize uint32) (hr error) {
	if hr = procCimWriteStream.Find(); hr != nil {
		return
	}
	r0, _, _ := syscall.Syscall(procCimWriteStream.Addr(), 3, uintptr(cimStreamHandle), uintptr(buffer), uintptr(bufferSize))
	if int32(r0) < 0 {
		if r0&0x1fff0000 == 0x00070000 {
			r0 &= 0xffff
		}
		hr = syscall.Errno(r0)
	}
	return
}

func CimDeletePath(cimFSHandle FsHandle, path string) (hr error) {
	var _p0 *uint16
	_p0, hr = syscall.UTF16PtrFromString(path)
	if hr != nil {
		return
	}
	return _CimDeletePath(cimFSHandle, _p0)
}

func _CimDeletePath(cimFSHandle FsHandle, path *uint16) (hr error) {
	if hr = procCimDeletePath.Find(); hr != nil {
		return
	}
	r0, _, _ := syscall.Syscall(procCimDeletePath.Addr(), 2, uintptr(cimFSHandle), uintptr(unsafe.Pointer(path)), 0)
	if int32(r0) < 0 {
		if r0&0x1fff0000 == 0x00070000 {
			r0 &= 0xffff
		}
		hr = syscall.Errno(r0)
	}
	return
}

func CimCreateHardLink(cimFSHandle FsHandle, newPath string, oldPath string) (hr error) {
	var _p0 *uint16
	_p0, hr = syscall.UTF16PtrFromString(newPath)
	if hr != nil {
		return
	}
	var _p1 *uint16
	_p1, hr = syscall.UTF16PtrFromString(oldPath)
	if hr != nil {
		return
	}
	return _CimCreateHardLink(cimFSHandle, _p0, _p1)
}

func _CimCreateHardLink(cimFSHandle FsHandle, newPath *uint16, oldPath *uint16) (hr error) {
	if hr = procCimCreateHardLink.Find(); hr != nil {
		return
	}
	r0, _, _ := syscall.Syscall(procCimCreateHardLink.Addr(), 3, uintptr(cimFSHandle), uintptr(unsafe.Pointer(newPath)), uintptr(unsafe.Pointer(oldPath)))
	if int32(r0) < 0 {
		if r0&0x1fff0000 == 0x00070000 {
			r0 &= 0xffff
		}
		hr = syscall.Errno(r0)
	}
	return
}

func CimCreateAlternateStream(cimFSHandle FsHandle, path string, size uint64, cimStreamHandle *StreamHandle) (hr error) {
	var _p0 *uint16
	_p0, hr = syscall.UTF16PtrFromString(path)
	if hr != nil {
		return
	}
	return _CimCreateAlternateStream(cimFSHandle, _p0, size, cimStreamHandle)
}

func _CimCreateAlternateStream(cimFSHandle FsHandle, path *uint16, size uint64, cimStreamHandle *StreamHandle) (hr error) {
	if hr = procCimCreateAlternateStream.Find(); hr != nil {
		return
	}
	r0, _, _ := syscall.Syscall6(procCimCreateAlternateStream.Addr(), 4, uintptr(cimFSHandle), uintptr(unsafe.Pointer(path)), uintptr(size), uintptr(unsafe.Pointer(cimStreamHandle)), 0, 0)
	if int32(r0) < 0 {
		if r0&0x1fff0000 == 0x00070000 {
			r0 &= 0xffff
		}
		hr = syscall.Errno(r0)
	}
	return
}
