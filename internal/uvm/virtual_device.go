package uvm

import (
	"context"
	"fmt"

	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/internal/guestrequest"
	"github.com/Microsoft/hcsshim/internal/requesttype"
	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/pkg/errors"
)

const (
	GPUDeviceIDType         = "gpu"
	VPCILocationPathIDType  = "vpci-location-path"
	VPCIClassGUIDTypeLegacy = "class"
	VPCIClassGUIDType       = "vpci-class-guid"
	VPCIDeviceIDTypeLegacy  = "vpci"
	VPCIDeviceIDType        = "vpci-instance-id"
)

// this is the well known channel type GUID defined by VMBUS for all assigned devices
const vmbusChannelTypeGUIDFormatted = "{44c4f61d-4444-4400-9d52-802e27ede19f}"
const assignedDeviceEnumerator = "VMBUS"

// VPCIDevice represents a vpci device. Holds its guid and a handle to the uvm it
// belongs to.
type VPCIDevice struct {
	// vm is the handle to the UVM that this device belongs to
	vm *UtilityVM
	// VMBusGUID is the instance ID for this device when it is exposed via VMBus
	VMBusGUID string
	// deviceInstanceID is the instance ID of the device on the host
	deviceInstanceID string
	// refCount stores the number of references to this device in the UVM
	refCount uint32
}

// GetAssignedDeviceVMBUSInstanceID returns the instance ID of the VMBUS channel device node created.
//
// When a device is assigned to a UVM via VPCI support in HCS, a new VMBUS channel device node is
// created in the UVM. The actual device that was assigned in is exposed as a child on this VMBUS
// channel device node.
//
// A device node's instance ID is an identifier that distinguishes that device from other devices
// on the system. The GUID of a VMBUS channel device node refers to that channel's unique
// identifier used internally by VMBUS and can be used to determine the VMBUS channel
// device node's instance ID.
//
// A VMBUS channel device node's instance ID is in the form:
// "VMBUS\vmbusChannelTypeGUIDFormatted\{vmBusChannelGUID}"
func (uvm *UtilityVM) GetAssignedDeviceVMBUSInstanceID(vmBusChannelGUID string) string {
	return fmt.Sprintf("%s\\%s\\{%s}", assignedDeviceEnumerator, vmbusChannelTypeGUIDFormatted, vmBusChannelGUID)
}

// Release frees the resources of the corresponding vpci device
func (vpci *VPCIDevice) Release(ctx context.Context) error {
	if err := vpci.vm.removeDevice(ctx, vpci.deviceInstanceID); err != nil {
		return fmt.Errorf("failed to remove VPCI device: %s", err)
	}
	return nil
}

// AssignDevice assigns a vpci device to the uvm
// if the device already exists, the stored VPCIDevice's ref count is increased
// and the VPCIDevice is returned.
// Otherwise, a new request is made to assign the target device indicated by the deviceID
// onto the UVM. A new VPCIDevice entry is made on the UVM and the VPCIDevice is returned
// to the caller
func (uvm *UtilityVM) AssignDevice(ctx context.Context, deviceID string) (*VPCIDevice, error) {
	guid, err := guid.NewV4()
	if err != nil {
		return nil, err
	}
	vmBusGUID := guid.String()

	uvm.m.Lock()
	defer uvm.m.Unlock()

	existingVPCIDevice := uvm.vpciDevices[deviceID]
	if existingVPCIDevice != nil {
		existingVPCIDevice.refCount++
		return existingVPCIDevice, nil
	}

	pci, ok := uvm.vm.(vm.PCIManager)
	if !ok || !uvm.vm.Supported(vm.PCI, vm.Add) {
		return nil, errors.Wrap(vm.ErrNotSupported, "stopping pci device add")
	}
	if err := pci.AddDevice(ctx, deviceID, vmBusGUID); err != nil {
		return nil, errors.Wrap(err, "failed to add vpci device")
	}

	// WCOW (when supported) does not require a guest request as part of the
	// device assignment
	if uvm.operatingSystem != "windows" {
		// For LCOW, we need to make sure that specific paths relating to the
		// device exist so they are ready to be used by later work in openGCS
		guestReq := guestrequest.GuestRequest{
			ResourceType: guestrequest.ResourceTypeVPCIDevice,
			RequestType:  requesttype.Add,
			Settings: guestrequest.LCOWMappedVPCIDevice{
				VMBusGUID: vmBusGUID,
			},
		}
		if err := uvm.GuestRequest(ctx, guestReq); err != nil {
			return nil, err
		}
	}

	result := &VPCIDevice{
		vm:               uvm,
		VMBusGUID:        vmBusGUID,
		deviceInstanceID: deviceID,
		refCount:         1,
	}
	uvm.vpciDevices[deviceID] = result
	return result, nil
}

// removeDevice removes a vpci device from a uvm when there are
// no more references to a given VPCIDevice. Otherwise, decrements
// the reference count of the stored VPCIDevice and returns nil.
func (uvm *UtilityVM) removeDevice(ctx context.Context, deviceInstanceID string) error {
	uvm.m.Lock()
	defer uvm.m.Unlock()

	vpci := uvm.vpciDevices[deviceInstanceID]
	if vpci == nil {
		return fmt.Errorf("no device with ID %s is present on the uvm %s", deviceInstanceID, uvm.ID())
	}

	vpci.refCount--
	if vpci.refCount == 0 {
		delete(uvm.vpciDevices, deviceInstanceID)

		pci, ok := uvm.vm.(vm.PCIManager)
		if !ok || !uvm.vm.Supported(vm.PCI, vm.Add) {
			return errors.Wrap(vm.ErrNotSupported, "stopping pci device removal")
		}
		if err := pci.RemoveDevice(ctx, vpci.deviceInstanceID, vpci.VMBusGUID); err != nil {
			return errors.Wrap(err, "failed to remove vpci device")
		}
	}
	return nil
}
