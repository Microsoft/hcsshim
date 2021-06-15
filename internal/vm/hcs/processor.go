package hcs

import (
	"context"

	"github.com/Microsoft/hcsshim/internal/hcs/resourcepaths"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/vm"
)

func (uvmb *utilityVMBuilder) SetProcessorCount(count uint32) error {
	uvmb.doc.VirtualMachine.ComputeTopology.Processor.Count = int32(count)
	return nil
}

func (uvmb *utilityVMBuilder) SetProcessorLimits(ctx context.Context, limits *vm.ProcessorLimits) error {
	uvmb.doc.VirtualMachine.ComputeTopology.Processor.Limit = int32(limits.Limit)
	uvmb.doc.VirtualMachine.ComputeTopology.Processor.Weight = int32(limits.Weight)
	return nil
}

func (uvm *utilityVM) SetProcessorCount(count uint32) error {
	return vm.ErrNotSupported
}

func vmProcessorLimitsToHCS(limits *vm.ProcessorLimits) *hcsschema.ProcessorLimits {
	return &hcsschema.ProcessorLimits{
		Limit:  limits.Limit,
		Weight: limits.Weight,
	}
}

func (uvm *utilityVM) SetProcessorLimits(ctx context.Context, limits *vm.ProcessorLimits) error {
	req := &hcsschema.ModifySettingRequest{
		ResourcePath: resourcepaths.CPULimitsResourcePath,
		Settings:     vmProcessorLimitsToHCS(limits),
	}
	return uvm.cs.Modify(ctx, req)
}
