package uvm

import (
	"context"

	"github.com/Microsoft/hcsshim/internal/guestrequest"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/requesttype"
)

// CombineLayersWCOW combines `layerPaths` with `containerRootPath` into the
// container file system.
//
// Note: `layerPaths` and `containerRootPath` are paths from within the UVM.
func (uvm *UtilityVM) CombineLayersWCOW(ctx context.Context, layerPaths []hcsschema.Layer, containerRootPath string) error {
	if uvm.operatingSystem != "windows" {
		return errNotSupported
	}
	guestReq := guestrequest.GuestRequest{
		ResourceType: guestrequest.ResourceTypeCombinedLayers,
		RequestType:  requesttype.Add,
		Settings: guestrequest.CombinedLayers{
			ContainerRootPath: containerRootPath,
			Layers:            layerPaths,
		},
	}
	return uvm.GuestRequest(ctx, guestReq)
}

// CombineLayersLCOW combines `layerPaths` and optionally `scratchPath` into an
// overlay filesystem at `rootfsPath`. If `scratchPath` is empty the overlay
// will be read only.
//
// NOTE: `layerPaths`, `scrathPath`, and `rootfsPath` are paths from within the
// UVM.
func (uvm *UtilityVM) CombineLayersLCOW(ctx context.Context, layerPaths []string, scratchPath, rootfsPath string) error {
	if uvm.operatingSystem != "linux" {
		return errNotSupported
	}

	layers := []hcsschema.Layer{}
	for _, l := range layerPaths {
		layers = append(layers, hcsschema.Layer{Path: l})
	}
	guestReq := guestrequest.GuestRequest{
		ResourceType: guestrequest.ResourceTypeCombinedLayers,
		RequestType:  requesttype.Add,
		Settings: guestrequest.CombinedLayers{
			ContainerRootPath: rootfsPath,
			Layers:            layers,
			ScratchPath:       scratchPath,
		},
	}
	return uvm.GuestRequest(ctx, guestReq)
}

// RemoveCombinedLayers removes the previously combined layers at `rootfsPath`.
//
// NOTE: `rootfsPath` is the path from within the UVM.
func (uvm *UtilityVM) RemoveCombinedLayers(ctx context.Context, rootfsPath string) error {
	guestReq := guestrequest.GuestRequest{
		ResourceType: guestrequest.ResourceTypeCombinedLayers,
		RequestType:  requesttype.Remove,
		Settings: guestrequest.CombinedLayers{
			ContainerRootPath: rootfsPath,
		},
	}
	return uvm.GuestRequest(ctx, guestReq)
}
