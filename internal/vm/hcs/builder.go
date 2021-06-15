package hcs

import (
	"context"

	"github.com/Microsoft/hcsshim/internal/hcs"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/logfields"
	"github.com/Microsoft/hcsshim/internal/schemaversion"
	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

var _ vm.UVMBuilder = &utilityVMBuilder{}

type utilityVMBuilder struct {
	id      string
	owner   string
	guestOS vm.GuestOS
	doc     *hcsschema.ComputeSystem
}

func NewUVMBuilder(id, owner string, guestOS vm.GuestOS) (vm.UVMBuilder, error) {
	doc := &hcsschema.ComputeSystem{
		Owner:                             owner,
		SchemaVersion:                     schemaversion.SchemaV21(),
		ShouldTerminateOnLastHandleClosed: true,
		VirtualMachine: &hcsschema.VirtualMachine{
			StopOnReset: true,
			Chipset:     &hcsschema.Chipset{},
			ComputeTopology: &hcsschema.Topology{
				Memory: &hcsschema.Memory2{
					AllowOvercommit: true,
				},
				Processor: &hcsschema.Processor2{},
			},
			Devices: &hcsschema.Devices{
				HvSocket: &hcsschema.HvSocket2{
					HvSocketConfig: &hcsschema.HvSocketSystemConfig{
						// Allow administrators and SYSTEM to bind to vsock sockets
						// so that we can create a GCS log socket.
						DefaultBindSecurityDescriptor: "D:P(A;;FA;;;SY)(A;;FA;;;BA)",
					},
				},
			},
		},
	}

	switch guestOS {
	case vm.Windows:
		doc.VirtualMachine.Devices.VirtualSmb = &hcsschema.VirtualSmb{
			DirectFileMappingInMB: 1024, // Sensible default, but could be a tuning parameter somewhere
		}
		doc.VirtualMachine.Chipset = &hcsschema.Chipset{
			Uefi: &hcsschema.Uefi{
				BootThis: &hcsschema.UefiBootEntry{
					DevicePath: `\EFI\Microsoft\Boot\bootmgfw.efi`,
					DeviceType: "VmbFs",
				},
			},
		}
	case vm.Linux:
		doc.VirtualMachine.Devices.Plan9 = &hcsschema.Plan9{}
	default:
		return nil, vm.ErrUnknownGuestOS
	}

	return &utilityVMBuilder{
		id:      id,
		owner:   owner,
		guestOS: guestOS,
		doc:     doc,
	}, nil
}

func (uvmb *utilityVMBuilder) Create(ctx context.Context, opts []vm.CreateOpt) (_ vm.UVM, err error) {
	// Apply any opts
	for _, o := range opts {
		if err := o(ctx, uvmb); err != nil {
			return nil, errors.Wrap(err, "failed applying create options for Utility VM")
		}
	}

	cs, err := hcs.CreateComputeSystem(ctx, uvmb.id, uvmb.doc)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create hcs compute system")
	}

	defer func() {
		if err != nil {
			_ = cs.Terminate(ctx)
			_ = cs.Wait()
		}
	}()

	backingType := vm.MemoryBackingTypeVirtual
	if !uvmb.doc.VirtualMachine.ComputeTopology.Memory.AllowOvercommit {
		backingType = vm.MemoryBackingTypePhysical
	}

	uvm := &utilityVM{
		id:          uvmb.id,
		owner:       uvmb.owner,
		guestOS:     uvmb.guestOS,
		cs:          cs,
		backingType: backingType,
	}

	properties, err := cs.Properties(ctx)
	if err != nil {
		return nil, err
	}
	uvm.vmID = properties.RuntimeID

	log.G(ctx).WithFields(logrus.Fields{
		logfields.UVMID: uvm.id,
		"runtime-id":    uvm.vmID,
	}).Debug("created utility VM")
	return uvm, nil
}
