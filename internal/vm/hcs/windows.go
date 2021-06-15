package hcs

import (
	"context"

	"github.com/Microsoft/hcsshim/internal/hcs/resourcepaths"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
)

func (uvmb *utilityVMBuilder) SetCPUGroup(ctx context.Context, id string) error {
	uvmb.doc.VirtualMachine.ComputeTopology.Processor.CpuGroup = &hcsschema.CpuGroup{Id: id}
	return nil
}

func (uvm *utilityVM) SetCPUGroup(ctx context.Context, id string) error {
	req := &hcsschema.ModifySettingRequest{
		ResourcePath: resourcepaths.CPUGroupResourcePath,
		Settings: &hcsschema.CpuGroup{
			Id: id,
		},
	}
	return uvm.cs.Modify(ctx, req)
}
