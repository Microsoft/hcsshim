package uvm

import (
	"context"

	"github.com/Microsoft/hcsshim/cmd/containerd-shim-runhcs-v1/stats"
)

// Stats returns various UVM statistics.
func (uvm *UtilityVM) Stats(ctx context.Context) (*stats.VirtualMachineStatistics, error) {
	return uvm.vm.Stats(ctx)
}
