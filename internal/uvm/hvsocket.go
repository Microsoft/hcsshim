package uvm

import (
	"context"

	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/pkg/errors"
)

// UpdateHvSocketService updates/creates the hvsocket service for the UVM. Takes in a service ID and
// the hvsocket service configuration. If there is no entry for the service ID already it will be created.
// The same call on HvSockets side handles the Create/Update/Delete cases based on what is passed in. Here is the logic
// for the call.
//
// 1. If the service ID does not currently exist in the service table, it will be created
// with whatever descriptors and state was specified (disabled or not).
// 2. If the service already exists and empty descriptors and Disabled is passed in for the
// service config, the service will be removed.
// 3. Otherwise any combination that is not Disabled && Empty descriptors will just update the
// service.
//
// If the request is crafted with Disabled = True and empty descriptors, then this function
// will remove the hvsocket service entry.
func (uvm *UtilityVM) UpdateHvSocketService(ctx context.Context, sid string, doc *vm.HvSocketServiceConfig) error {
	vmsocket, ok := uvm.vm.(vm.VMSocketManager)
	if !ok {
		return errors.Wrap(vm.ErrNotSupported, "stopping vmsocket operation")
	}
	return vmsocket.UpdateVMSocket(ctx, vm.HvSocket, sid, doc)
}
