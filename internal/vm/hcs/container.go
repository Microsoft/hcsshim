package hcs

import (
	"context"

	"github.com/Microsoft/hcsshim/internal/cow"
	"github.com/Microsoft/hcsshim/internal/hcs"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/schemaversion"
)

// These handle the case where we don't have a direct

func (uvm *utilityVM) CreateContainer(ctx context.Context, config interface{}) (cow.Container, error) {
	doc := hcsschema.ComputeSystem{
		HostingSystemId:                   uvm.id,
		Owner:                             uvm.owner,
		SchemaVersion:                     schemaversion.SchemaV21(),
		ShouldTerminateOnLastHandleClosed: true,
		HostedSystem:                      config,
	}
	return hcs.CreateComputeSystem(ctx, uvm.id, &doc)
}

func (uvm *utilityVM) CreateProcess(ctx context.Context, config interface{}) (cow.Process, error) {
	return uvm.cs.CreateProcess(ctx, config)
}
