package uvm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Microsoft/go-winio"
	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/internal/gcs"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/logfields"
	"github.com/Microsoft/hcsshim/internal/ncproxyttrpc"
	"github.com/Microsoft/hcsshim/internal/oc"
	"github.com/Microsoft/hcsshim/internal/processorinfo"
	"github.com/Microsoft/hcsshim/internal/uvmfolder"
	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/Microsoft/hcsshim/internal/vm/hcs"
	"github.com/Microsoft/hcsshim/internal/vm/remotevm"
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
	log.G(ctx).WithField("vmID", uvm.vm.VmID()).Debug("Using external GCS bridge")

	vmsocket, ok := uvm.vm.(vm.VMSocketManager)
	if !ok {
		return errors.Wrap(vm.ErrNotSupported, "stopping vm socket operation")
	}

	l, err := vmsocket.VMSocketListen(ctx, vm.HvSocket, gcs.WindowsGcsHvsockServiceID)
	if err != nil {
		return err
	}
	uvm.gcListener = l
	return nil
}

func prepareConfigDoc(ctx context.Context, uvm *UtilityVM, opts *OptionsWCOW, uvmFolder string) error {
	processorTopology, err := processorinfo.HostProcessorInfo(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to get host processor information")
	}

	// To maintain compatability with Docker we need to automatically downgrade
	// a user CPU count if the setting is not possible.
	uvm.processorCount = uvm.normalizeProcessorCount(ctx, opts.ProcessorCount, processorTopology)

	// Align the requested memory size.
	memorySizeInMB := uvm.normalizeMemorySize(ctx, opts.MemorySizeInMB)

	mem, ok := uvm.builder.(vm.MemoryManager)
	if !ok {
		return errors.Wrap(vm.ErrNotSupported, "stopping memory setup")
	}

	if err := mem.SetMemoryLimit(ctx, memorySizeInMB); err != nil {
		return errors.Wrap(err, "failed to set memory limit")
	}

	backingType := vm.MemoryBackingTypeVirtual
	if !opts.AllowOvercommit {
		backingType = vm.MemoryBackingTypePhysical
	}

	if err := mem.SetMemoryConfig(&vm.MemoryConfig{
		BackingType:    backingType,
		DeferredCommit: opts.EnableDeferredCommit,
		HotHint:        opts.AllowOvercommit,
	}); err != nil {
		return errors.Wrap(err, "failed to set memory config")
	}

	vsmb, ok := uvm.builder.(vm.VSMBManager)
	if !ok {
		return errors.Wrap(vm.ErrNotSupported, "stopping VSMB operation")
	}
	// UVM rootfs share is readonly.
	vsmbOpts := uvm.DefaultVSMBOptions(true)
	vsmbOpts.TakeBackupPrivilege = true
	if opts.IsTemplate {
		uvm.SetSaveableVSMBOptions(vsmbOpts, vsmbOpts.ReadOnly)
	}

	if err := vsmb.AddVSMB(
		ctx,
		filepath.Join(uvmFolder, `UtilityVM\Files`),
		"os",
		nil,
		vsmbOpts,
	); err != nil {
		return errors.Wrap(err, "failed to set VSMB share on UVM document")
	}

	cpu, ok := uvm.builder.(vm.ProcessorManager)
	if !ok {
		return errors.Wrap(vm.ErrNotSupported, "stopping cpu operation")
	}

	limits := &vm.ProcessorLimits{
		Limit:  uint64(opts.ProcessorLimit),
		Weight: uint64(opts.ProcessorWeight),
	}
	if err := cpu.SetProcessorLimits(ctx, limits); err != nil {
		return errors.Wrap(err, "failed to set processor limit on UVM document")
	}
	if err := cpu.SetProcessorCount(uint32(uvm.processorCount)); err != nil {
		return errors.Wrap(err, "failed to set processor count on UVM document")
	}

	// We can set a cpu group for the VM at creation time in recent builds.
	if opts.CPUGroupID != "" {
		if osversion.Build() < cpuGroupCreateBuild {
			return errCPUGroupCreateNotSupported
		}
		windows, ok := uvm.builder.(vm.WindowsConfigManager)
		if !ok {
			return errors.Wrap(vm.ErrNotSupported, "stopping cpu groups operation")
		}
		if err := windows.SetCPUGroup(ctx, opts.CPUGroupID); err != nil {
			return err
		}
	}

	storage, ok := uvm.builder.(vm.StorageQosManager)
	if !ok {
		return errors.Wrap(vm.ErrNotSupported, "stopping storage qos operation")
	}

	// Handle StorageQoS if set
	if opts.StorageQoSBandwidthMaximum > 0 || opts.StorageQoSIopsMaximum > 0 {
		if err := storage.SetStorageQos(
			int64(opts.StorageQoSIopsMaximum),
			int64(opts.StorageQoSBandwidthMaximum),
		); err != nil {
			return err
		}
	}

	return nil
}

// CreateWCOW creates a Windows utility VM.
// The UVM can either be created from scratch or can be cloned from a template.
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

	var (
		uvmb  vm.UVMBuilder
		cOpts []vm.CreateOpt
	)
	switch opts.VMSource {
	case vm.HCS:
		uvmb, err = hcs.NewUVMBuilder(uvm.id, uvm.owner, vm.Windows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create UVM builder")
		}
		cOpts = applyHcsOpts(opts)
	case vm.RemoteVM:
		uvmb, err = remotevm.NewUVMBuilder(ctx, uvm.id, uvm.owner, opts.VMServicePath, opts.VMServiceAddress, vm.Windows)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create UVM builder")
		}
		cOpts = applyRemoteVMOpts(opts.Options)
	default:
		return nil, fmt.Errorf("unknown VM source: %s", opts.VMSource)
	}
	uvm.builder = uvmb

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

	if err := prepareConfigDoc(ctx, uvm, opts, uvmFolder); err != nil {
		return nil, errors.Wrap(err, "error in preparing config doc")
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

		scsi, ok := uvm.builder.(vm.SCSIManager)
		if !ok {
			return nil, errors.Wrap(vm.ErrNotSupported, "stopping scsi operation")
		}
		if err := scsi.AddSCSIController(0); err != nil {
			return nil, err
		}
		if err := scsi.AddSCSIDisk(ctx, 0, 0, scratchPath, vm.SCSIDiskTypeVHDX, false); err != nil {
			return nil, err
		}

		uvm.scsiLocations[0][0] = newSCSIMount(uvm, scratchPath, "", "", 1, 0, 0, false)
	} else {
		for _, cloneableResource := range opts.TemplateConfig.Resources {
			err = cloneableResource.Clone(ctx, uvm, &cloneData{
				builder:       uvmb,
				scratchFolder: scratchFolder,
				uvmID:         opts.ID,
			})
			if err != nil {
				return nil, errors.Wrap(err, "failed while cloning resource")
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
	uvm.IsTemplate = opts.IsTemplate

	uvm.vm, err = uvmb.Create(ctx, cOpts)
	if err != nil {
		return nil, errors.Wrap(err, "error while creating the Utility VM: %s")
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
