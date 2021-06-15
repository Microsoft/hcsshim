package hcs

import (
	"context"

	"github.com/Microsoft/hcsshim/internal/hcs/resourcepaths"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/requesttype"
	"github.com/Microsoft/hcsshim/internal/vm"
)

func (uvmb *utilityVMBuilder) AddVSMB(ctx context.Context, path string, name string, allowed []string, options *vm.VSMBOptions) error {
	if uvmb.doc.VirtualMachine.Devices.VirtualSmb == nil {
		uvmb.doc.VirtualMachine.Devices.VirtualSmb = &hcsschema.VirtualSmb{}
	}

	uvmb.doc.VirtualMachine.Devices.VirtualSmb.Shares = append(uvmb.doc.VirtualMachine.Devices.VirtualSmb.Shares,
		hcsschema.VirtualSmbShare{
			Name:         name,
			Path:         path,
			AllowedFiles: allowed,
			Options:      vmVSMBOptionsToHCS(options),
		},
	)
	return nil
}

func (uvmb *utilityVMBuilder) RemoveVSMB(ctx context.Context, name string) error {
	return vm.ErrNotSupported
}

func vmVSMBOptionsToHCS(options *vm.VSMBOptions) *hcsschema.VirtualSmbShareOptions {
	return &hcsschema.VirtualSmbShareOptions{
		ReadOnly:            options.ReadOnly,
		ShareRead:           options.ShareRead,
		CacheIo:             options.CacheIo,
		NoOplocks:           options.NoOplocks,
		NoDirectmap:         options.NoDirectMap,
		TakeBackupPrivilege: options.TakeBackupPrivilege,
		PseudoOplocks:       options.PseudoOplocks,
		PseudoDirnotify:     options.PseudoDirnotify,
		NoLocks:             options.NoLocks,
		RestrictFileAccess:  options.RestrictFileAccess,
		SingleFileMapping:   options.SingleFileMapping,
	}
}

func (uvm *utilityVM) AddVSMB(ctx context.Context, path string, name string, allowed []string, options *vm.VSMBOptions) error {
	modification := &hcsschema.ModifySettingRequest{
		RequestType: requesttype.Add,
		Settings: hcsschema.VirtualSmbShare{
			Name:         name,
			Options:      vmVSMBOptionsToHCS(options),
			Path:         path,
			AllowedFiles: allowed,
		},
		ResourcePath: resourcepaths.VSMBShareResourcePath,
	}
	return uvm.cs.Modify(ctx, modification)
}

func (uvm *utilityVM) RemoveVSMB(ctx context.Context, name string) error {
	modification := &hcsschema.ModifySettingRequest{
		RequestType:  requesttype.Remove,
		Settings:     hcsschema.VirtualSmbShare{Name: name},
		ResourcePath: resourcepaths.VSMBShareResourcePath,
	}
	return uvm.cs.Modify(ctx, modification)
}
