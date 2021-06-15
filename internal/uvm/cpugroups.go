package uvm

import (
	"context"
	"fmt"

	"github.com/Microsoft/hcsshim/internal/cpugroup"
	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/pkg/errors"
)

// Build that assigning a cpu group on creation of a vm is supported
const cpuGroupCreateBuild = 20124

var errCPUGroupCreateNotSupported = fmt.Errorf("cpu group assignment on create requires a build of %d or higher", cpuGroupCreateBuild)

// ReleaseCPUGroup unsets the cpugroup from the VM
func (uvm *UtilityVM) ReleaseCPUGroup(ctx context.Context) error {
	if err := uvm.unsetCPUGroup(ctx); err != nil {
		return fmt.Errorf("failed to remove VM %s from cpugroup", uvm.ID())
	}
	return nil
}

// SetCPUGroup setups up the cpugroup for the VM with the requested id
func (uvm *UtilityVM) SetCPUGroup(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("must specify an ID to use when configuring a VM's cpugroup")
	}
	return uvm.setCPUGroup(ctx, id)
}

// setCPUGroup sets the VM's cpugroup
func (uvm *UtilityVM) setCPUGroup(ctx context.Context, id string) error {
	windows, ok := uvm.vm.(vm.WindowsConfigManager)
	if !ok {
		return errors.Wrap(vm.ErrNotSupported, "stopping cpu group operation")
	}
	return windows.SetCPUGroup(ctx, id)
}

// unsetCPUGroup sets the VM's cpugroup to the null group ID
// set groupID to 00000000-0000-0000-0000-000000000000 to remove the VM from a cpugroup
//
// Since a VM must be moved to the null group before potentially being added to a different
// cpugroup, that means there may be a segment of time that the VM's cpu usage runs unrestricted.
func (uvm *UtilityVM) unsetCPUGroup(ctx context.Context) error {
	return uvm.setCPUGroup(ctx, cpugroup.NullGroupID)
}
