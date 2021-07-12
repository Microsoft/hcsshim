package uvm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Microsoft/go-winio"
	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/internal/gcs"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/logfields"
	"github.com/Microsoft/hcsshim/internal/ncproxyttrpc"
	"github.com/Microsoft/hcsshim/internal/oc"
	"github.com/Microsoft/hcsshim/internal/processorinfo"
	"github.com/Microsoft/hcsshim/internal/schemaversion"
	"github.com/Microsoft/hcsshim/internal/uvmfolder"
	"github.com/Microsoft/hcsshim/internal/wclayer"
	"github.com/Microsoft/hcsshim/internal/wcow"
	"github.com/Microsoft/hcsshim/osversion"
	"github.com/containerd/ttrpc"
	"github.com/pkg/errors"
	"go.opencensus.io/trace"
)

// OptionsWCOW are the set of options passed to CreateWCOW() to create a utility vm.
type OptionsWCOW struct {
	*Options

	LayerFolders []string // Set of folders for base layers and scratch. Ordered from top most read-only through base read-only layer, followed by scratch

	// IsTemplate specifies if this UVM will be saved as a template in future. Setting
	// this option will also enable some VSMB Options during UVM creation that allow
	// template creation.
	IsTemplate bool

	// IsClone specifies if this UVM should be created by cloning a template. If
	// IsClone is true then a valid UVMTemplateConfig struct must be passed in the
	// `TemplateConfig` field.
	IsClone bool

	// TemplateConfig is only used during clone creation. If a uvm is
	// being cloned then this TemplateConfig struct must be passed
	// which holds all the information about the template from
	// which this clone should be created.
	TemplateConfig *UVMTemplateConfig

	// NoDirectMap specifies that no direct mapping should be used for any VSMBs added to the UVM
	NoDirectMap bool
}

// NewDefaultOptionsWCOW creates the default options for a bootable version of
// WCOW. The caller `MUST` set the `LayerFolders` path on the returned value.
//
// `id` the ID of the compute system. If not passed will generate a new GUID.
//
// `owner` the owner of the compute system. If not passed will use the
// executable files name.
func NewDefaultOptionsWCOW(id, owner string) *OptionsWCOW {
	return &OptionsWCOW{
		Options: newDefaultOptions(id, owner),
	}
}

func (uvm *UtilityVM) startExternalGcsListener(ctx context.Context) error {
	log.G(ctx).WithField("vmID", uvm.runtimeID).Debug("Using external GCS bridge")

	l, err := winio.ListenHvsock(&winio.HvsockAddr{
		VMID:      uvm.runtimeID,
		ServiceID: gcs.WindowsGcsHvsockServiceID,
	})
	if err != nil {
		return err
	}
	uvm.gcListener = l
	return nil
}

func prepareConfigDoc(ctx context.Context, uvm *UtilityVM, opts *OptionsWCOW, uvmFolder string) (*hcsschema.ComputeSystem, error) {
	processorTopology, err := processorinfo.HostProcessorInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get host processor information: %s", err)
	}

	// To maintain compatability with Docker we need to automatically downgrade
	// a user CPU count if the setting is not possible.
	uvm.processorCount = uvm.normalizeProcessorCount(ctx, opts.ProcessorCount, processorTopology)

	// Align the requested memory size.
	memorySizeInMB := uvm.normalizeMemorySize(ctx, opts.MemorySizeInMB)

	// UVM rootfs share is readonly.
	vsmbOpts := uvm.DefaultVSMBOptions(true)
	vsmbOpts.TakeBackupPrivilege = true
	virtualSMB := &hcsschema.VirtualSmb{
		DirectFileMappingInMB: 1024, // Sensible default, but could be a tuning parameter somewhere
		Shares: []hcsschema.VirtualSmbShare{
			{
				Name:    "os",
				Path:    filepath.Join(uvmFolder, `UtilityVM\Files`),
				Options: vsmbOpts,
			},
		},
	}

	// Here for a temporary workaround until the need for setting this regkey is no more. To protect
	// against any undesired behavior (such as some general networking scenarios ceasing to function)
	// with a recent change to fix SMB share access in the UVM, this registry key will be checked to
	// enable the change in question inside GNS.dll.
	var registryChanges hcsschema.RegistryChanges
	if !opts.DisableCompartmentNamespace {
		registryChanges = hcsschema.RegistryChanges{
			AddValues: []hcsschema.RegistryValue{
				{
					Key: &hcsschema.RegistryKey{
						Hive: "System",
						Name: "CurrentControlSet\\Services\\gns",
					},
					Name:       "EnableCompartmentNamespace",
					DWordValue: 1,
					Type_:      "DWord",
				},
			},
		}
	}

	processor := &hcsschema.Processor2{
		Count:  uvm.processorCount,
		Limit:  opts.ProcessorLimit,
		Weight: opts.ProcessorWeight,
	}
	// We can set a cpu group for the VM at creation time in recent builds.
	if opts.CPUGroupID != "" {
		if osversion.Build() < cpuGroupCreateBuild {
			return nil, errCPUGroupCreateNotSupported
		}
		processor.CpuGroup = &hcsschema.CpuGroup{Id: opts.CPUGroupID}
	}

	doc := &hcsschema.ComputeSystem{
		Owner:                             uvm.owner,
		SchemaVersion:                     schemaversion.SchemaV21(),
		ShouldTerminateOnLastHandleClosed: true,
		VirtualMachine: &hcsschema.VirtualMachine{
			StopOnReset: true,
			Chipset: &hcsschema.Chipset{
				Uefi: &hcsschema.Uefi{
					BootThis: &hcsschema.UefiBootEntry{
						DevicePath: `\EFI\Microsoft\Boot\bootmgfw.efi`,
						DeviceType: "VmbFs",
					},
				},
			},
			RegistryChanges: &registryChanges,
			ComputeTopology: &hcsschema.Topology{
				Memory: &hcsschema.Memory2{
					SizeInMB:        memorySizeInMB,
					AllowOvercommit: opts.AllowOvercommit,
					// EnableHotHint is not compatible with physical.
					EnableHotHint:        opts.AllowOvercommit,
					EnableDeferredCommit: opts.EnableDeferredCommit,
					LowMMIOGapInMB:       opts.LowMMIOGapInMB,
					HighMMIOBaseInMB:     opts.HighMMIOBaseInMB,
					HighMMIOGapInMB:      opts.HighMMIOGapInMB,
				},
				Processor: processor,
			},
			Devices: &hcsschema.Devices{
				HvSocket: &hcsschema.HvSocket2{
					HvSocketConfig: &hcsschema.HvSocketSystemConfig{
						// Allow administrators and SYSTEM to bind to vsock sockets
						// so that we can create a GCS log socket.
						DefaultBindSecurityDescriptor: "D:P(A;;FA;;;SY)(A;;FA;;;BA)",
					},
				},
				VirtualSmb: virtualSMB,
			},
		},
	}

	// Handle StorageQoS if set
	if opts.StorageQoSBandwidthMaximum > 0 || opts.StorageQoSIopsMaximum > 0 {
		doc.VirtualMachine.StorageQoS = &hcsschema.StorageQoS{
			IopsMaximum:      opts.StorageQoSIopsMaximum,
			BandwidthMaximum: opts.StorageQoSBandwidthMaximum,
		}
	}

	return doc, nil
}

// CreateWCOW creates an HCS compute system representing a utility VM.
// The HCS Compute system can either be created from scratch or can be cloned from a
// template.
//
// WCOW Notes:
//   - The scratch is always attached to SCSI 0:0
//
func CreateWCOW(ctx context.Context, opts *OptionsWCOW) (_ *UtilityVM, err error) {
	ctx, span := trace.StartSpan(ctx, "uvm::CreateWCOW")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()

	if opts.ID == "" {
		g, err := guid.NewV4()
		if err != nil {
			return nil, err
		}
		opts.ID = g.String()
	}

	span.AddAttributes(trace.StringAttribute(logfields.UVMID, opts.ID))
	log.G(ctx).WithField("options", fmt.Sprintf("%+v", opts)).Debug("uvm::CreateWCOW options")

	uvm := &UtilityVM{
		id:                      opts.ID,
		owner:                   opts.Owner,
		operatingSystem:         "windows",
		scsiControllerCount:     1,
		vsmbDirShares:           make(map[string]*VSMBShare),
		vsmbFileShares:          make(map[string]*VSMBShare),
		vpciDevices:             make(map[string]*VPCIDevice),
		physicallyBacked:        !opts.AllowOvercommit,
		devicesPhysicallyBacked: opts.FullyPhysicallyBacked,
		vsmbNoDirectMap:         opts.NoDirectMap,
		createOpts:              *opts,
	}

	defer func() {
		if err != nil {
			uvm.Close()
		}
	}()

	if err := verifyOptions(ctx, opts); err != nil {
		return nil, errors.Wrap(err, errBadUVMOpts.Error())
	}

	uvmFolder, err := uvmfolder.LocateUVMFolder(ctx, opts.LayerFolders)
	if err != nil {
		return nil, fmt.Errorf("failed to locate utility VM folder from layer folders: %s", err)
	}

	// TODO: BUGBUG Remove this. @jhowardmsft
	//       It should be the responsiblity of the caller to do the creation and population.
	//       - Update runhcs too (vm.go).
	//       - Remove comment in function header
	//       - Update tests that rely on this current behaviour.
	// Create the RW scratch in the top-most layer folder, creating the folder if it doesn't already exist.
	scratchFolder := opts.LayerFolders[len(opts.LayerFolders)-1]

	// Create the directory if it doesn't exist
	if _, err := os.Stat(scratchFolder); os.IsNotExist(err) {
		if err := os.MkdirAll(scratchFolder, 0777); err != nil {
			return nil, fmt.Errorf("failed to create utility VM scratch folder: %s", err)
		}
	}

	doc, err := prepareConfigDoc(ctx, uvm, opts, uvmFolder)
	if err != nil {
		return nil, fmt.Errorf("error in preparing config doc: %s", err)
	}

	if !opts.IsClone {
		// Create sandbox.vhdx in the scratch folder based on the template, granting the correct permissions to it
		scratchPath := filepath.Join(scratchFolder, "sandbox.vhdx")
		if _, err := os.Stat(scratchPath); os.IsNotExist(err) {
			if err := wcow.CreateUVMScratch(ctx, uvmFolder, scratchFolder, uvm.id); err != nil {
				return nil, fmt.Errorf("failed to create scratch: %s", err)
			}
		} else {
			// Sandbox.vhdx exists, just need to grant vm access to it.
			if err := wclayer.GrantVmAccess(ctx, uvm.id, scratchPath); err != nil {
				return nil, errors.Wrap(err, "failed to grant vm access to scratch")
			}
		}

		doc.VirtualMachine.Devices.Scsi = map[string]hcsschema.Scsi{
			"0": {
				Attachments: map[string]hcsschema.Attachment{
					"0": {
						Path:  scratchPath,
						Type_: "VirtualDisk",
					},
				},
			},
		}

		uvm.scsiLocations[0][0] = newSCSIMount(uvm,
			doc.VirtualMachine.Devices.Scsi["0"].Attachments["0"].Path,
			"",
			doc.VirtualMachine.Devices.Scsi["0"].Attachments["0"].Type_,
			"", 1, 0, 0, false)
	} else {
		doc.VirtualMachine.RestoreState = &hcsschema.RestoreState{}
		doc.VirtualMachine.RestoreState.TemplateSystemId = opts.TemplateConfig.UVMID

		for _, cloneableResource := range opts.TemplateConfig.Resources {
			err = cloneableResource.Clone(ctx, uvm, &cloneData{
				doc:           doc,
				scratchFolder: scratchFolder,
				uvmID:         opts.ID,
			})
			if err != nil {
				return nil, fmt.Errorf("failed while cloning: %s", err)
			}
		}

		// we add default clone namespace for each clone. Include it here.
		if uvm.namespaces == nil {
			uvm.namespaces = make(map[string]*namespaceInfo)
		}
		uvm.namespaces[DEFAULT_CLONE_NETWORK_NAMESPACE_ID] = &namespaceInfo{
			nics: make(map[string]*nicInfo),
		}
		uvm.IsClone = true
		uvm.TemplateID = opts.TemplateConfig.UVMID
	}

	// Add appropriate VSMB share options if this UVM needs to be saved as a template
	if opts.IsTemplate {
		for _, share := range doc.VirtualMachine.Devices.VirtualSmb.Shares {
			uvm.SetSaveableVSMBOptions(share.Options, share.Options.ReadOnly)
		}
		uvm.IsTemplate = true
	}

	err = uvm.create(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("error while creating the compute system: %s", err)
	}

	if err = uvm.startExternalGcsListener(ctx); err != nil {
		return nil, err
	}

	// If network config proxy address passed in, construct a client.
	if opts.NetworkConfigProxy != "" {
		conn, err := winio.DialPipe(opts.NetworkConfigProxy, nil)
		if err != nil {
			return nil, errors.Wrap(err, "failed to connect to ncproxy service")
		}
		client := ttrpc.NewClient(conn, ttrpc.WithOnClose(func() { conn.Close() }))
		uvm.ncProxyClient = ncproxyttrpc.NewNetworkConfigProxyClient(client)
	}

	return uvm, nil
}
