package hcs

import (
	"context"

	"github.com/Microsoft/hcsshim/internal/hcs/resourcepaths"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/vm"
)

func (uvmb *utilityVMBuilder) SetMemoryLimit(ctx context.Context, memoryMB uint64) error {
	uvmb.doc.VirtualMachine.ComputeTopology.Memory.SizeInMB = memoryMB
	return nil
}

func (uvmb *utilityVMBuilder) SetMemoryConfig(config *vm.MemoryConfig) error {
	memory := uvmb.doc.VirtualMachine.ComputeTopology.Memory
	memory.AllowOvercommit = config.BackingType == vm.MemoryBackingTypeVirtual
	memory.EnableDeferredCommit = config.DeferredCommit
	memory.EnableHotHint = config.HotHint
	memory.EnableColdHint = config.ColdHint
	memory.EnableColdDiscardHint = config.ColdDiscardHint
	return nil
}

func (uvmb *utilityVMBuilder) SetMMIOConfig(lowGapMB uint64, highBaseMB uint64, highGapMB uint64) error {
	memory := uvmb.doc.VirtualMachine.ComputeTopology.Memory
	memory.LowMMIOGapInMB = lowGapMB
	memory.HighMMIOBaseInMB = highBaseMB
	memory.HighMMIOGapInMB = highGapMB
	return nil
}

func (uvm *utilityVM) SetMemoryLimit(ctx context.Context, memoryMB uint64) error {
	req := &hcsschema.ModifySettingRequest{
		ResourcePath: resourcepaths.MemoryResourcePath,
		Settings:     memoryMB,
	}
	return uvm.cs.Modify(ctx, req)
}
