package uvm

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Microsoft/hcsshim/internal/guestrequest"
	"github.com/Microsoft/hcsshim/internal/requesttype"
	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/Microsoft/hcsshim/osversion"
	"github.com/pkg/errors"
)

// Plan9Share is a struct containing host paths for the UVM
type Plan9Share struct {
	// UVM resource belongs to
	vm            *UtilityVM
	name, uvmPath string
}

// Release frees the resources of the corresponding Plan9 share
func (p9 *Plan9Share) Release(ctx context.Context) error {
	if err := p9.vm.RemovePlan9(ctx, p9); err != nil {
		return errors.Wrap(err, "failed to remove plan9 share")
	}
	return nil
}

const plan9Port = 564

// AddPlan9 adds a Plan9 share to a utility VM.
func (uvm *UtilityVM) AddPlan9(ctx context.Context, hostPath string, uvmPath string, readOnly bool, restrict bool, allowedNames []string) (*Plan9Share, error) {
	if uvm.operatingSystem != "linux" {
		return nil, errNotSupported
	}
	if restrict && osversion.Build() < osversion.V19H1 {
		return nil, errors.New("single-file mappings are not supported on this build of Windows")
	}
	if uvmPath == "" {
		return nil, fmt.Errorf("uvmPath must be passed to AddPlan9")
	}

	// TODO: JTERRY75 - These are marked private in the schema. For now use them
	// but when there are public variants we need to switch to them.
	const (
		shareFlagsReadOnly           int32 = 0x00000001
		shareFlagsLinuxMetadata      int32 = 0x00000004
		shareFlagsCaseSensitive      int32 = 0x00000008
		shareFlagsRestrictFileAccess int32 = 0x00000080
	)

	// TODO: JTERRY75 - `shareFlagsCaseSensitive` only works if the Windows
	// `hostPath` supports case sensitivity. We need to detect this case before
	// forwarding this flag in all cases.
	flags := shareFlagsLinuxMetadata // | shareFlagsCaseSensitive
	if readOnly {
		flags |= shareFlagsReadOnly
	}
	if restrict {
		flags |= shareFlagsRestrictFileAccess
	}

	uvm.m.Lock()
	index := uvm.plan9Counter
	uvm.plan9Counter++
	uvm.m.Unlock()
	name := strconv.FormatUint(index, 10)

	plan9, ok := uvm.vm.(vm.Plan9Manager)
	if !ok || !uvm.vm.Supported(vm.Plan9, vm.Add) {
		return nil, errors.Wrap(vm.ErrNotSupported, "stopping plan 9 share add")
	}

	if err := plan9.AddPlan9(ctx, hostPath, name, plan9Port, flags, allowedNames); err != nil {
		return nil, errors.Wrap(err, "failed to add plan 9 share")
	}

	guestReq := guestrequest.GuestRequest{
		ResourceType: guestrequest.ResourceTypeMappedDirectory,
		RequestType:  requesttype.Add,
		Settings: guestrequest.LCOWMappedDirectory{
			MountPath: uvmPath,
			ShareName: name,
			Port:      plan9Port,
			ReadOnly:  readOnly,
		},
	}

	if err := uvm.GuestRequest(ctx, guestReq); err != nil {
		return nil, err
	}

	return &Plan9Share{
		vm:      uvm,
		name:    name,
		uvmPath: uvmPath,
	}, nil
}

// RemovePlan9 removes a Plan9 share from a utility VM. Each Plan9 share is ref-counted
// and only actually removed when the ref-count drops to zero.
func (uvm *UtilityVM) RemovePlan9(ctx context.Context, share *Plan9Share) error {
	if uvm.operatingSystem != "linux" {
		return errNotSupported
	}

	plan9, ok := uvm.vm.(vm.Plan9Manager)
	if !ok || !uvm.vm.Supported(vm.Plan9, vm.Remove) {
		return errors.Wrap(vm.ErrNotSupported, "stopping plan 9 share removal")
	}

	guestReq := guestrequest.GuestRequest{
		ResourceType: guestrequest.ResourceTypeMappedDirectory,
		RequestType:  requesttype.Remove,
		Settings: guestrequest.LCOWMappedDirectory{
			MountPath: share.uvmPath,
			ShareName: share.name,
			Port:      plan9Port,
		},
	}

	if err := uvm.GuestRequest(ctx, guestReq); err != nil {
		return fmt.Errorf("failed to remove plan9 share %s from %s: %+v: %s", share.name, uvm.id, guestReq, err)
	}
	if err := plan9.RemovePlan9(ctx, share.name, plan9Port); err != nil {
		return errors.Wrap(err, "failed to remove plan 9 share")
	}
	return nil
}
