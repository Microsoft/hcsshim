package remotevm

import (
	"context"

	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/Microsoft/hcsshim/internal/vmservice"
)

func (uvmb *utilityVMBuilder) SetProcessorCount(count uint32) error {
	if uvmb.config.ProcessorConfig == nil {
		uvmb.config.ProcessorConfig = &vmservice.ProcessorConfig{}
	}
	uvmb.config.ProcessorConfig.ProcessorCount = count
	return nil
}

func (uvmb *utilityVMBuilder) SetProcessorLimits(ctx context.Context, limits *vm.ProcessorLimits) error {
	return vm.ErrNotSupported
}
