package uvm

import (
	"context"

	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/pkg/errors"
)

// UpdateCPULimits updates the CPU limits of the utility vm
func (uvm *UtilityVM) UpdateCPULimits(ctx context.Context, limits *vm.ProcessorLimits) error {
	cpu, ok := uvm.vm.(vm.ProcessorManager)
	if !ok || !uvm.vm.Supported(vm.Processor, vm.Update) {
		return errors.Wrap(vm.ErrNotSupported, "stopping update of cpus")
	}
	return cpu.SetProcessorLimits(ctx, limits)
}
