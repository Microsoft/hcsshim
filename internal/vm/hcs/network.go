package hcs

import (
	"context"
	"fmt"

	"github.com/Microsoft/hcsshim/internal/hcs/resourcepaths"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/requesttype"
	"github.com/Microsoft/hcsshim/internal/vm"
)

func (uvm *utilityVM) AddNIC(ctx context.Context, nicID, endpointID, macAddr string) error {
	request := hcsschema.ModifySettingRequest{
		RequestType:  requesttype.Add,
		ResourcePath: fmt.Sprintf(resourcepaths.NetworkResourceFormat, nicID),
		Settings: hcsschema.NetworkAdapter{
			EndpointId: endpointID,
			MacAddress: macAddr,
		},
	}
	return uvm.cs.Modify(ctx, request)
}

func (uvm *utilityVM) RemoveNIC(ctx context.Context, nicID, endpointID, macAddr string) error {
	request := hcsschema.ModifySettingRequest{
		RequestType:  requesttype.Remove,
		ResourcePath: fmt.Sprintf(resourcepaths.NetworkResourceFormat, nicID),
		Settings: hcsschema.NetworkAdapter{
			EndpointId: endpointID,
			MacAddress: macAddr,
		},
	}
	return uvm.cs.Modify(ctx, request)
}

func (uvm *utilityVM) UpdateNIC(ctx context.Context, nicID string, nic *vm.NetworkAdapter) error {
	moderationName := hcsschema.InterruptModerationName(*nic.IovSettings.InterruptModeration)
	req := &hcsschema.ModifySettingRequest{
		RequestType:  requesttype.Update,
		ResourcePath: fmt.Sprintf(resourcepaths.NetworkResourceFormat, nicID),
		Settings: hcsschema.NetworkAdapter{
			EndpointId: nic.EndpointId,
			MacAddress: nic.MacAddress,
			IovSettings: &hcsschema.IovSettings{
				OffloadWeight:       nic.IovSettings.OffloadWeight,
				QueuePairsRequested: nic.IovSettings.QueuePairsRequested,
				InterruptModeration: &moderationName,
			},
		},
	}
	return uvm.cs.Modify(ctx, req)
}
