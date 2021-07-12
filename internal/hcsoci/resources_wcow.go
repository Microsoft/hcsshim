// +build windows

package hcsoci

// Contains functions relating to a WCOW container, as opposed to a utility VM

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Microsoft/hcsshim/internal/credentials"
	"github.com/Microsoft/hcsshim/internal/layers"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/resources"
	"github.com/Microsoft/hcsshim/internal/schemaversion"
	"github.com/Microsoft/hcsshim/internal/uvm"
	"github.com/Microsoft/hcsshim/internal/wclayer"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

func allocateWindowsResources(ctx context.Context, coi *createOptionsInternal, r *resources.Resources, isSandbox bool) error {
	if coi.Spec == nil || coi.Spec.Windows == nil || coi.Spec.Windows.LayerFolders == nil {
		return errors.New("field 'Spec.Windows.Layerfolders' is not populated")
	}

	scratchFolder := coi.Spec.Windows.LayerFolders[len(coi.Spec.Windows.LayerFolders)-1]

	// TODO: Remove this code for auto-creation. Make the caller responsible.
	// Create the directory for the RW scratch layer if it doesn't exist
	if _, err := os.Stat(scratchFolder); os.IsNotExist(err) {
		if err := os.MkdirAll(scratchFolder, 0777); err != nil {
			return errors.Wrapf(err, "failed to auto-create container scratch folder %s", scratchFolder)
		}
	}

	// Create sandbox.vhdx if it doesn't exist in the scratch folder. It's called sandbox.vhdx
	// rather than scratch.vhdx as in the v1 schema, it's hard-coded in HCS.
	if _, err := os.Stat(filepath.Join(scratchFolder, "sandbox.vhdx")); os.IsNotExist(err) {
		if err := wclayer.CreateScratchLayer(ctx, scratchFolder, coi.Spec.Windows.LayerFolders[:len(coi.Spec.Windows.LayerFolders)-1]); err != nil {
			return errors.Wrap(err, "failed to CreateSandboxLayer")
		}
	}

	if coi.Spec.Root == nil {
		coi.Spec.Root = &specs.Root{}
	}

	if coi.Spec.Root.Path == "" && (coi.HostingSystem != nil || coi.Spec.Windows.HyperV == nil) {
		log.G(ctx).Debug("hcsshim::allocateWindowsResources mounting storage")
		containerRootInUVM := r.ContainerRootInUVM()
		containerRootPath, err := layers.MountContainerLayers(ctx, coi.Spec.Windows.LayerFolders, containerRootInUVM, "", coi.HostingSystem)
		if err != nil {
			return errors.Wrap(err, "failed to mount container storage")
		}
		coi.Spec.Root.Path = containerRootPath
		layers := layers.NewImageLayers(coi.HostingSystem, containerRootInUVM, coi.Spec.Windows.LayerFolders, "", isSandbox)
		r.SetLayers(layers)
	}

	if err := setupMounts(ctx, coi, r); err != nil {
		return err
	}

	if cs, ok := coi.Spec.Windows.CredentialSpec.(string); ok {
		// Only need to create a CCG instance for v2 containers
		if schemaversion.IsV21(coi.actualSchemaVersion) {
			hypervisorIsolated := coi.HostingSystem != nil
			ccgInstance, ccgResource, err := credentials.CreateCredentialGuard(ctx, coi.actualID, cs, hypervisorIsolated)
			if err != nil {
				return err
			}
			coi.ccgState = ccgInstance.CredentialGuard
			r.Add(ccgResource)
			if hypervisorIsolated {
				// If hypervisor isolated we need to add an hvsocket service table entry
				// By default HVSocket won't allow something inside the VM to connect
				// back to a process on the host. We need to update the HVSocket service table
				// to allow a connection to CCG.exe on the host, so that GMSA can function.
				// We need to hot add this here because at UVM creation time we don't know what containers
				// will be launched in the UVM, nonetheless if they will ask for GMSA. This is a workaround
				// for the previous design requirement for CCG V2 where the service entry
				// must be present in the UVM'S HCS document before being sent over as hot adding
				// an HvSocket service was not possible.
				hvSockConfig := ccgInstance.HvSocketConfig
				if err := coi.HostingSystem.UpdateHvSocketService(ctx, hvSockConfig.ServiceId, hvSockConfig.ServiceConfig); err != nil {
					return errors.Wrap(err, "failed to update hvsocket service")
				}
			}
		}
	}

	if coi.HostingSystem != nil && coi.hasWindowsAssignedDevices() {
		windowsDevices, closers, err := handleAssignedDevicesWindows(ctx, coi.HostingSystem, coi.Spec.Annotations, coi.Spec.Windows.Devices)
		if err != nil {
			return err
		}
		r.Add(closers...)
		coi.Spec.Windows.Devices = windowsDevices
	}

	return nil
}

// setupMounts adds the custom mounts requested in the container configuration of this
// request.
func setupMounts(ctx context.Context, coi *createOptionsInternal, r *resources.Resources) error {
	// Validate each of the mounts. If this is a V2 Xenon, we have to add them as
	// VSMB shares to the utility VM. For V1 Xenon and Argons, there's nothing for
	// us to do as it's done by HCS.
	for _, mount := range coi.Spec.Mounts {
		if mount.Destination == "" || mount.Source == "" {
			return fmt.Errorf("invalid OCI spec - a mount must have both source and a destination: %+v", mount)
		}
		switch mount.Type {
		case "":
		case "physical-disk":
		case "virtual-disk":
		case "extensible-virtual-disk":
		default:
			return fmt.Errorf("invalid OCI spec - Type '%s' not supported", mount.Type)
		}

		if coi.HostingSystem != nil && schemaversion.IsV21(coi.actualSchemaVersion) {
			uvmPath := fmt.Sprintf(uvm.WCOWGlobalMountPrefix, coi.HostingSystem.UVMMountCounter())
			readOnly := false
			for _, o := range mount.Options {
				if strings.ToLower(o) == "ro" {
					readOnly = true
					break
				}
			}
			l := log.G(ctx).WithField("mount", fmt.Sprintf("%+v", mount))
			if mount.Type == "physical-disk" {
				l.Debug("hcsshim::allocateWindowsResources Hot-adding SCSI physical disk for OCI mount")
				scsiMount, err := coi.HostingSystem.AddSCSIPhysicalDisk(ctx, mount.Source, uvmPath, readOnly, mount.Options)
				if err != nil {
					return errors.Wrapf(err, "adding SCSI physical disk mount %+v", mount)
				}
				r.Add(scsiMount)
			} else if mount.Type == "virtual-disk" {
				l.Debug("hcsshim::allocateWindowsResources Hot-adding SCSI virtual disk for OCI mount")
				scsiMount, err := coi.HostingSystem.AddSCSI(ctx, mount.Source, uvmPath, readOnly, mount.Options, uvm.VMAccessTypeIndividual)
				if err != nil {
					return errors.Wrapf(err, "adding SCSI virtual disk mount %+v", mount)
				}
				r.Add(scsiMount)
			} else if mount.Type == "extensible-virtual-disk" {
				l.Debug("hcsshim::allocateWindowsResource Hot-adding ExtensibleVirtualDisk")
				scsiMount, err := coi.HostingSystem.AddSCSIExtensibleVirtualDisk(ctx, mount.Source, uvmPath, readOnly)
				if err != nil {
					return errors.Wrapf(err, "adding SCSI EVD mount failed %+v", mount)
				}
				r.Add(scsiMount)
			} else {
				if uvm.IsPipe(mount.Source) {
					pipe, err := coi.HostingSystem.AddPipe(ctx, mount.Source)
					if err != nil {
						return errors.Wrap(err, "failed to add named pipe to UVM")
					}
					r.Add(pipe)
				} else {
					l.Debug("hcsshim::allocateWindowsResources Hot-adding VSMB share for OCI mount")
					options := coi.HostingSystem.DefaultVSMBOptions(readOnly)
					share, err := coi.HostingSystem.AddVSMB(ctx, mount.Source, options)
					if err != nil {
						return errors.Wrapf(err, "failed to add VSMB share to utility VM for mount %+v", mount)
					}
					r.Add(share)
				}
			}
		}
	}

	return nil
}
