package remotevm

import (
	"context"
	"sync"

	"github.com/Microsoft/hcsshim/internal/jobobject"
	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/Microsoft/hcsshim/internal/vmservice"
	ptypes "github.com/gogo/protobuf/types"
	"github.com/pkg/errors"
)

var _ vm.UVM = &utilityVM{}

type utilityVM struct {
	id              string
	waitBlock       chan struct{}
	closedWaitOnce  sync.Once
	waitError       error
	ignoreSupported bool
	job             *jobobject.JobObject
	config          *vmservice.VMConfig
	client          vmservice.VMService
	capabilities    *vmservice.CapabilitiesVMResponse
}

var vmSupportedResourceToVMService = map[vm.Resource]vmservice.CapabilitiesVMResponse_Resource{
	vm.VPMem:     vmservice.CapabilitiesVMResponse_Vpmem,
	vm.SCSI:      vmservice.CapabilitiesVMResponse_Scsi,
	vm.PCI:       vmservice.CapabilitiesVMResponse_Vpci,
	vm.Plan9:     vmservice.CapabilitiesVMResponse_Plan9,
	vm.Network:   vmservice.CapabilitiesVMResponse_VMNic,
	vm.Memory:    vmservice.CapabilitiesVMResponse_Memory,
	vm.Processor: vmservice.CapabilitiesVMResponse_Processor,
}

func (uvm *utilityVM) ID() string {
	return uvm.id
}

func (uvm *utilityVM) VmID() string {
	return ""
}

func (uvm *utilityVM) Start(ctx context.Context) error {
	// The expectation is the VM should be in a paused state after creation.
	if _, err := uvm.client.ResumeVM(ctx, &ptypes.Empty{}); err != nil {
		return errors.Wrap(err, "failed to start remote VM")
	}
	return nil
}

func (uvm *utilityVM) Stop(ctx context.Context) error {
	if _, err := uvm.client.TeardownVM(ctx, &ptypes.Empty{}); err != nil {
		return errors.Wrap(err, "failed to stop remote VM")
	}
	return nil
}

func (uvm *utilityVM) Close() error {
	err := uvm.job.Close()
	uvm.closedWaitOnce.Do(func() {
		close(uvm.waitBlock)
	})
	return err
}

func (uvm *utilityVM) Wait() error {
	<-uvm.waitBlock
	return uvm.waitError
}

func (uvm *utilityVM) waitBackground() {
	_, err := uvm.client.WaitVM(context.Background(), &ptypes.Empty{})
	uvm.closedWaitOnce.Do(func() {
		uvm.waitError = err
		close(uvm.waitBlock)
	})
}

func (uvm *utilityVM) Pause(ctx context.Context) error {
	if _, err := uvm.client.PauseVM(ctx, &ptypes.Empty{}); err != nil {
		return errors.Wrap(err, "failed to pause remote VM")
	}
	return nil
}

func (uvm *utilityVM) Resume(ctx context.Context) error {
	if _, err := uvm.client.ResumeVM(ctx, &ptypes.Empty{}); err != nil {
		return errors.Wrap(err, "failed to resume remote VM")
	}
	return nil
}

func (uvm *utilityVM) Save(ctx context.Context) error {
	return vm.ErrNotSupported
}

func (uvm *utilityVM) Supported(resource vm.Resource, operation vm.ResourceOperation) bool {
	if uvm.ignoreSupported {
		return true
	}
	var foundResource *vmservice.CapabilitiesVMResponse_SupportedResource
	for _, supportedResource := range uvm.capabilities.SupportedResources {
		if vmSupportedResourceToVMService[resource] == supportedResource.Resource {
			foundResource = supportedResource
		}
	}

	supported := false
	switch operation {
	case vm.Add:
		supported = foundResource.Add
	case vm.Remove:
		supported = foundResource.Remove
	case vm.Update:
		supported = foundResource.Update
	default:
		return false
	}

	return supported
}

func (uvm *utilityVM) ExitError() error {
	return uvm.waitError
}
