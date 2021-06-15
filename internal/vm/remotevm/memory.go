package remotevm

import (
	"context"

	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/Microsoft/hcsshim/internal/vmservice"
)

func (uvmb *utilityVMBuilder) SetMemoryLimit(ctx context.Context, memoryMB uint64) error {
	if uvmb.config.MemoryConfig == nil {
		uvmb.config.MemoryConfig = &vmservice.MemoryConfig{}
	}
	uvmb.config.MemoryConfig.MemoryMb = memoryMB
	return nil
}

func (uvmb *utilityVMBuilder) SetMemoryConfig(config *vm.MemoryConfig) error {
	if uvmb.config.MemoryConfig == nil {
		uvmb.config.MemoryConfig = &vmservice.MemoryConfig{}
	}
	uvmb.config.MemoryConfig.AllowOvercommit = config.BackingType == vm.MemoryBackingTypeVirtual
	uvmb.config.MemoryConfig.ColdHint = config.ColdHint
	uvmb.config.MemoryConfig.ColdDiscardHint = config.ColdDiscardHint
	uvmb.config.MemoryConfig.DeferredCommit = config.DeferredCommit
	uvmb.config.MemoryConfig.HotHint = config.HotHint
	return nil
}

func (uvmb *utilityVMBuilder) SetMMIOConfig(lowGapMB uint64, highBaseMB uint64, highGapMB uint64) error {
	if uvmb.config.MemoryConfig == nil {
		uvmb.config.MemoryConfig = &vmservice.MemoryConfig{}
	}
	uvmb.config.MemoryConfig.HighMmioBaseInMb = highBaseMB
	uvmb.config.MemoryConfig.LowMmioGapInMb = lowGapMB
	uvmb.config.MemoryConfig.HighMmioGapInMb = highGapMB
	return nil
}
