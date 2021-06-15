package uvm

import (
	"context"
	"fmt"

	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/pkg/errors"
)

const (
	bytesPerPage = 4096
	bytesPerMB   = 1024 * 1024
)

// UpdateMemory makes a call to the VM's orchestrator to update the VM's size in MB
// Internally, HCS will get the number of pages this corresponds to and attempt to assign
// pages to numa nodes evenly
func (uvm *UtilityVM) UpdateMemory(ctx context.Context, sizeInBytes uint64) error {
	requestedSizeInMB := sizeInBytes / bytesPerMB
	actual := uvm.normalizeMemorySize(ctx, requestedSizeInMB)
	mem, ok := uvm.vm.(vm.MemoryManager)
	if !ok || !uvm.vm.Supported(vm.Memory, vm.Update) {
		return errors.Wrap(vm.ErrNotSupported, "stopping update of memory")
	}
	return mem.SetMemoryLimit(ctx, actual)
}

// GetAssignedMemoryInBytes returns the amount of assigned memory for the UVM in bytes
func (uvm *UtilityVM) GetAssignedMemoryInBytes(ctx context.Context) (uint64, error) {
	stats, err := uvm.vm.Stats(ctx)
	if err != nil {
		return 0, errors.Wrap(err, "failed to fetch Utility VM stats")
	}
	if stats.Memory == nil {
		return 0, fmt.Errorf("no memory properties returned for system %s", uvm.id)
	}

	if stats.Memory.VmMemory == nil {
		return 0, fmt.Errorf("no virtual memory properties returned for system %s", uvm.id)
	}
	pages := stats.Memory.VmMemory.AssignedMemory
	if pages == 0 {
		return 0, fmt.Errorf("assigned memory returned should not be 0 for system %s", uvm.id)
	}
	memInBytes := pages * bytesPerPage
	return memInBytes, nil
}
