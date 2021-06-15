package uvm

import (
	"context"

	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/vm"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

func (uvm *UtilityVM) UpdateConstraints(ctx context.Context, data interface{}, annotations map[string]string) error {
	var memoryLimitInBytes *uint64
	var processorLimits *vm.ProcessorLimits

	switch resources := data.(type) {
	case *specs.WindowsResources:
		if resources.Memory != nil {
			memoryLimitInBytes = resources.Memory.Limit
		}
		if resources.CPU != nil {
			processorLimits := &hcsschema.ProcessorLimits{}
			if resources.CPU.Maximum != nil {
				processorLimits.Limit = uint64(*resources.CPU.Maximum)
			}
			if resources.CPU.Shares != nil {
				processorLimits.Weight = uint64(*resources.CPU.Shares)
			}
		}
	case *specs.LinuxResources:
		if resources.Memory != nil {
			mem := uint64(*resources.Memory.Limit)
			memoryLimitInBytes = &mem
		}
		if resources.CPU != nil {
			processorLimits := &hcsschema.ProcessorLimits{}
			if resources.CPU.Quota != nil {
				processorLimits.Limit = uint64(*resources.CPU.Quota)
			}
			if resources.CPU.Shares != nil {
				processorLimits.Weight = uint64(*resources.CPU.Shares)
			}
		}
	}

	if memoryLimitInBytes != nil {
		if err := uvm.UpdateMemory(ctx, *memoryLimitInBytes); err != nil {
			return err
		}
	}
	if processorLimits != nil {
		if err := uvm.UpdateCPULimits(ctx, processorLimits); err != nil {
			return err
		}
	}
	return nil
}
