package uvm

import (
	"context"
	"fmt"
	"os"

	"github.com/Microsoft/hcsshim/internal/guestrequest"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/requesttype"
	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	lcowVPMEMLayerFmt = "/run/layers/p%d"
)

var (
	// ErrMaxVPMEMLayerSize is the error returned when the size of `hostPath` is
	// greater than the max vPMEM layer size set at create time.
	ErrMaxVPMEMLayerSize = errors.New("layer size is to large for VPMEM max size")
)

// findNextVPMEM finds the next available VPMem slot.
//
// The lock MUST be held when calling this function.
func (uvm *UtilityVM) findNextVPMEM(ctx context.Context, hostPath string) (uint32, error) {
	for i := uint32(0); i < uvm.vpmemMaxCount; i++ {
		if uvm.vpmemDevices[i] == nil {
			log.G(ctx).WithFields(logrus.Fields{
				"hostPath":     hostPath,
				"deviceNumber": i,
			}).Debug("allocated VPMEM location")
			return i, nil
		}
	}
	return 0, ErrNoAvailableLocation
}

// Lock must be held when calling this function
func (uvm *UtilityVM) findVPMEMDevice(ctx context.Context, findThisHostPath string) (uint32, error) {
	for i := uint32(0); i < uvm.vpmemMaxCount; i++ {
		if vi := uvm.vpmemDevices[i]; vi != nil && vi.hostPath == findThisHostPath {
			log.G(ctx).WithFields(logrus.Fields{
				"hostPath":     vi.hostPath,
				"uvmPath":      vi.uvmPath,
				"refCount":     vi.refCount,
				"deviceNumber": i,
			}).Debug("found VPMEM location")
			return i, nil
		}
	}
	return 0, ErrNotAttached
}

// AddVPMEM adds a VPMEM disk to a utility VM at the next available location and
// returns the UVM path where the layer was mounted.
func (uvm *UtilityVM) AddVPMEM(ctx context.Context, hostPath string) (_ string, err error) {
	if uvm.operatingSystem != "linux" {
		return "", errNotSupported
	}

	uvm.m.Lock()
	defer uvm.m.Unlock()

	var deviceNumber uint32
	deviceNumber, err = uvm.findVPMEMDevice(ctx, hostPath)
	if err != nil {
		// We are going to add it so make sure it fits on vPMEM
		fi, err := os.Stat(hostPath)
		if err != nil {
			return "", err
		}
		if uint64(fi.Size()) > uvm.vpmemMaxSizeBytes {
			return "", ErrMaxVPMEMLayerSize
		}

		// It doesn't exist, so we're going to allocate and hot-add it
		deviceNumber, err = uvm.findNextVPMEM(ctx, hostPath)
		if err != nil {
			return "", err
		}

		vpmem, ok := uvm.vm.(vm.VPMemManager)
		if !ok || !uvm.vm.Supported(vm.VPMem, vm.Add) {
			return "", errors.Wrap(vm.ErrNotSupported, "stopping vpmem device add")
		}

		if err := vpmem.AddVPMemDevice(ctx, deviceNumber, hostPath, true, vm.VPMemImageFormatVHD1); err != nil {
			return "", errors.Wrap(err, "failed to add vpmem device")
		}

		uvmPath := fmt.Sprintf(lcowVPMEMLayerFmt, deviceNumber)
		guestReq := guestrequest.GuestRequest{
			ResourceType: guestrequest.ResourceTypeVPMemDevice,
			RequestType:  requesttype.Add,
			Settings: guestrequest.LCOWMappedVPMemDevice{
				DeviceNumber: deviceNumber,
				MountPath:    uvmPath,
			},
		}

		if err := uvm.GuestRequest(ctx, guestReq); err != nil {
			return "", errors.Wrap(err, "failed guest request to add vpmem device")
		}

		uvm.vpmemDevices[deviceNumber] = &vpmemInfo{
			hostPath: hostPath,
			uvmPath:  uvmPath,
			refCount: 1,
		}
		return uvmPath, nil
	}
	device := uvm.vpmemDevices[deviceNumber]
	device.refCount++
	return device.uvmPath, nil
}

// RemoveVPMEM removes a VPMEM disk from a Utility VM. If the `hostPath` is not
// attached returns `ErrNotAttached`.
func (uvm *UtilityVM) RemoveVPMEM(ctx context.Context, hostPath string) (err error) {
	if uvm.operatingSystem != "linux" {
		return errNotSupported
	}

	uvm.m.Lock()
	defer uvm.m.Unlock()

	deviceNumber, err := uvm.findVPMEMDevice(ctx, hostPath)
	if err != nil {
		return err
	}

	device := uvm.vpmemDevices[deviceNumber]
	if device.refCount == 1 {
		vpmem, ok := uvm.vm.(vm.VPMemManager)
		if !ok || !uvm.vm.Supported(vm.VPMem, vm.Remove) {
			return errors.Wrap(vm.ErrNotSupported, "stopping vpmem device removal")
		}

		guestReq := guestrequest.GuestRequest{
			ResourceType: guestrequest.ResourceTypeVPMemDevice,
			RequestType:  requesttype.Remove,
			Settings: guestrequest.LCOWMappedVPMemDevice{
				DeviceNumber: deviceNumber,
				MountPath:    device.uvmPath,
			},
		}

		if err := uvm.GuestRequest(ctx, guestReq); err != nil {
			return errors.Wrap(err, "failed to remove vpmem device from guest")
		}

		if err := vpmem.RemoveVPMemDevice(ctx, deviceNumber, hostPath); err != nil {
			return errors.Wrap(err, "failed to remove vpmem device")
		}

		log.G(ctx).WithFields(logrus.Fields{
			"hostPath":     device.hostPath,
			"uvmPath":      device.uvmPath,
			"refCount":     device.refCount,
			"deviceNumber": deviceNumber,
		}).Debug("removed VPMEM location")
		uvm.vpmemDevices[deviceNumber] = nil
	} else {
		device.refCount--
	}
	return nil
}
