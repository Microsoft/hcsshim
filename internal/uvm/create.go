package uvm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Microsoft/hcsshim/internal/cow"
	"github.com/Microsoft/hcsshim/internal/hcs"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/logfields"
	"github.com/Microsoft/hcsshim/internal/oc"
	"github.com/Microsoft/hcsshim/internal/schemaversion"
	"github.com/Microsoft/hcsshim/osversion"
	"github.com/sirupsen/logrus"
	"go.opencensus.io/trace"
	"golang.org/x/sys/windows"
)

// Options are the set of options passed to Create() to create a utility vm.
type Options struct {
	ID    string // Identifier for the uvm. Defaults to generated GUID.
	Owner string // Specifies the owner. Defaults to executable name.

	// MemorySizeInMB sets the UVM memory. If `0` will default to platform
	// default.
	MemorySizeInMB uint64

	LowMMIOGapInMB   uint64
	HighMMIOBaseInMB uint64
	HighMMIOGapInMB  uint64

	// Memory for UVM. Defaults to true. For physical backed memory, set to
	// false.
	AllowOvercommit bool

	// FullyPhysicallyBacked describes if a uvm should be entirely physically
	// backed, including in any additional devices
	FullyPhysicallyBacked bool

	// Memory for UVM. Defaults to false. For virtual memory with deferred
	// commit, set to true.
	EnableDeferredCommit bool

	// ProcessorCount sets the number of vCPU's. If `0` will default to platform
	// default.
	ProcessorCount int32

	// ProcessorLimit sets the maximum percentage of each vCPU's the UVM can
	// consume. If `0` will default to platform default.
	ProcessorLimit int32

	// ProcessorWeight sets the relative weight of these vCPU's vs another UVM's
	// when scheduling. If `0` will default to platform default.
	ProcessorWeight int32

	// StorageQoSIopsMaximum sets the maximum number of Iops. If `0` will
	// default to the platform default.
	StorageQoSIopsMaximum int32

	// StorageQoSIopsMaximum sets the maximum number of bytes per second. If `0`
	// will default to the platform default.
	StorageQoSBandwidthMaximum int32

	// DisableCompartmentNamespace sets whether to disable namespacing the network compartment in the UVM
	// for WCOW. Namespacing makes it so the compartment created for a container is essentially no longer
	// aware or able to see any of the other compartments on the host (in this case the UVM).
	// The compartment that the container is added to now behaves as the default compartment as
	// far as the container is concerned and it is only able to view the NICs in the compartment it's assigned to.
	// This is the compartment setup (and behavior) that is followed for V1 HCS schema containers (docker) so
	// this change brings parity as well. This behavior is gated behind a registry key currently to avoid any
	// unneccessary behavior and once this restriction is removed then we can remove the need for this variable
	// and the associated annotation as well.
	DisableCompartmentNamespace bool

	// CPUGroupID set the ID of a CPUGroup on the host that the UVM should be added to on start.
	// Defaults to an empty string which indicates the UVM should not be added to any CPUGroup.
	CPUGroupID string
	// NetworkConfigProxy holds the address of the network config proxy service.
	// This != "" determines whether to start the ComputeAgent TTRPC service
	// that receives the UVMs set of NICs from this proxy instead of enumerating
	// the endpoints locally.
	NetworkConfigProxy string

	// Sets the location for process dumps to be placed in. On Linux this is a kernel setting so it will be
	// applied to all containers. On Windows it's configurable per container, but we can mimic this for
	// Windows by just applying the location specified here per container.
	ProcessDumpLocation string
}

// compares the create opts used during template creation with the create opts
// provided for clone creation. If they don't match (except for a few fields)
// then clone creation is failed.
func verifyCloneUvmCreateOpts(templateOpts, cloneOpts *OptionsWCOW) bool {
	// Following fields can be different in the template and clone configurations.
	// 1. the scratch layer path. i.e the last element of the LayerFolders path.
	// 2. IsTemplate, IsClone and TemplateConfig variables.
	// 3. ID
	// 4. AdditionalHCSDocumentJSON

	// Save the original values of the fields that we want to ignore and replace them with
	// the same values as that of the other object. So that we can simply use `==` operator.
	templateIDBackup := templateOpts.ID
	templateOpts.ID = cloneOpts.ID

	// We can't use `==` operator on structs which include slices in them. So compare the
	// Layerfolders separately and then directly compare the Options struct.
	result := (len(templateOpts.LayerFolders) == len(cloneOpts.LayerFolders))
	for i := 0; result && i < len(templateOpts.LayerFolders)-1; i++ {
		result = result && (templateOpts.LayerFolders[i] == cloneOpts.LayerFolders[i])
	}
	result = result && (*templateOpts.Options == *cloneOpts.Options)

	// set original values
	templateOpts.ID = templateIDBackup
	return result
}

// Verifies that the final UVM options are correct and supported.
func verifyOptions(ctx context.Context, options interface{}) error {
	switch opts := options.(type) {
	case *OptionsLCOW:
		if opts.EnableDeferredCommit && !opts.AllowOvercommit {
			return errors.New("EnableDeferredCommit is not supported on physically backed VMs")
		}
		if opts.SCSIControllerCount > 1 {
			return errors.New("SCSI controller count must be 0 or 1") // Future extension here for up to 4
		}
		if opts.VPMemDeviceCount > MaxVPMEMCount {
			return fmt.Errorf("VPMem device count cannot be greater than %d", MaxVPMEMCount)
		}
		if opts.VPMemDeviceCount > 0 {
			if opts.VPMemSizeBytes%4096 != 0 {
				return errors.New("VPMemSizeBytes must be a multiple of 4096")
			}
		} else {
			if opts.PreferredRootFSType == PreferredRootFSTypeVHD {
				return errors.New("PreferredRootFSTypeVHD requires at least one VPMem device")
			}
		}
		if opts.KernelDirect && osversion.Build() < 18286 {
			return errors.New("KernelDirectBoot is not supported on builds older than 18286")
		}

		if opts.EnableColdDiscardHint && osversion.Build() < 18967 {
			return errors.New("EnableColdDiscardHint is not supported on builds older than 18967")
		}
	case *OptionsWCOW:
		if opts.EnableDeferredCommit && !opts.AllowOvercommit {
			return errors.New("EnableDeferredCommit is not supported on physically backed VMs")
		}
		if len(opts.LayerFolders) < 2 {
			return errors.New("at least 2 LayerFolders must be supplied")
		}
		if opts.IsClone && !verifyCloneUvmCreateOpts(&opts.TemplateConfig.CreateOpts, opts) {
			return errors.New("clone configuration doesn't match with template configuration.")
		}
		if opts.IsClone && opts.TemplateConfig == nil {
			return errors.New("template config can not be nil when creating clone")
		}
		if opts.IsTemplate && opts.FullyPhysicallyBacked {
			return errors.New("Template can not be created from a full physically backed UVM")
		}
	}
	return nil
}

// newDefaultOptions returns the default base options for WCOW and LCOW.
//
// If `id` is empty it will be generated.
//
// If `owner` is empty it will be set to the calling executables name.
func newDefaultOptions(id, owner string) *Options {
	opts := &Options{
		ID:                    id,
		Owner:                 owner,
		MemorySizeInMB:        1024,
		AllowOvercommit:       true,
		EnableDeferredCommit:  false,
		ProcessorCount:        defaultProcessorCount(),
		FullyPhysicallyBacked: false,
	}

	if opts.Owner == "" {
		opts.Owner = filepath.Base(os.Args[0])
	}

	return opts
}

// ID returns the ID of the VM's compute system.
func (uvm *UtilityVM) ID() string {
	return uvm.hcsSystem.ID()
}

// OS returns the operating system of the utility VM.
func (uvm *UtilityVM) OS() string {
	return uvm.operatingSystem
}

func (uvm *UtilityVM) create(ctx context.Context, doc interface{}) error {
	uvm.exitCh = make(chan struct{})
	system, err := hcs.CreateComputeSystem(ctx, uvm.id, doc)
	if err != nil {
		return err
	}
	defer func() {
		if system != nil {
			_ = system.Terminate(ctx)
			_ = system.Wait()
		}
	}()

	// Cache the VM ID of the utility VM.
	properties, err := system.Properties(ctx)
	if err != nil {
		return err
	}
	uvm.runtimeID = properties.RuntimeID
	uvm.hcsSystem = system
	system = nil

	log.G(ctx).WithFields(logrus.Fields{
		logfields.UVMID: uvm.id,
		"runtime-id":    uvm.runtimeID.String(),
	}).Debug("created utility VM")
	return nil
}

// Close terminates and releases resources associated with the utility VM.
func (uvm *UtilityVM) Close() (err error) {
	ctx, span := trace.StartSpan(context.Background(), "uvm::Close")
	defer span.End()
	defer func() { oc.SetSpanStatus(span, err) }()
	span.AddAttributes(trace.StringAttribute(logfields.UVMID, uvm.id))

	windows.Close(uvm.vmmemProcess)

	if uvm.hcsSystem != nil {
		_ = uvm.hcsSystem.Terminate(ctx)
		_ = uvm.Wait()
	}

	if err := uvm.CloseGCSConnection(); err != nil {
		log.G(ctx).Errorf("close GCS connection failed: %s", err)
	}

	// outputListener will only be nil for a Create -> Stop without a Start. In
	// this case we have no goroutine processing output so its safe to close the
	// channel here.
	if uvm.outputListener != nil {
		close(uvm.outputProcessingDone)
		uvm.outputListener.Close()
		uvm.outputListener = nil
	}
	if uvm.hcsSystem != nil {
		return uvm.hcsSystem.Close()
	}
	return nil
}

// CreateContainer creates a container in the utility VM.
func (uvm *UtilityVM) CreateContainer(ctx context.Context, id string, settings interface{}) (cow.Container, error) {
	if uvm.gc != nil {
		c, err := uvm.gc.CreateContainer(ctx, id, settings)
		if err != nil {
			return nil, fmt.Errorf("failed to create container %s: %s", id, err)
		}
		return c, nil
	}
	doc := hcsschema.ComputeSystem{
		HostingSystemId:                   uvm.id,
		Owner:                             uvm.owner,
		SchemaVersion:                     schemaversion.SchemaV21(),
		ShouldTerminateOnLastHandleClosed: true,
		HostedSystem:                      settings,
	}
	c, err := hcs.CreateComputeSystem(ctx, id, &doc)
	if err != nil {
		return nil, err
	}
	return c, err
}

// CreateProcess creates a process in the utility VM.
func (uvm *UtilityVM) CreateProcess(ctx context.Context, settings interface{}) (cow.Process, error) {
	if uvm.gc != nil {
		return uvm.gc.CreateProcess(ctx, settings)
	}
	return uvm.hcsSystem.CreateProcess(ctx, settings)
}

// IsOCI returns false, indicating the parameters to CreateProcess should not
// include an OCI spec.
func (uvm *UtilityVM) IsOCI() bool {
	return false
}

// Terminate requests that the utility VM be terminated.
func (uvm *UtilityVM) Terminate(ctx context.Context) error {
	return uvm.hcsSystem.Terminate(ctx)
}

// ExitError returns an error if the utility VM has terminated unexpectedly.
func (uvm *UtilityVM) ExitError() error {
	return uvm.hcsSystem.ExitError()
}

func defaultProcessorCount() int32 {
	if runtime.NumCPU() == 1 {
		return 1
	}
	return 2
}

// normalizeProcessorCount sets `uvm.processorCount` to `Min(requested,
// logical CPU count)`.
func (uvm *UtilityVM) normalizeProcessorCount(ctx context.Context, requested int32, processorTopology *hcsschema.ProcessorTopology) int32 {
	// Use host processor information retrieved from HCS instead of runtime.NumCPU,
	// GetMaximumProcessorCount or other OS level calls for two reasons.
	// 1. Go uses GetProcessAffinityMask and falls back to GetSystemInfo both of
	// which will not return LPs in another processor group.
	// 2. GetMaximumProcessorCount will return all processors on the system
	// but in configurations where the host partition doesn't see the full LP count
	// i.e "Minroot" scenarios this won't be sufficient.
	// (https://docs.microsoft.com/en-us/windows-server/virtualization/hyper-v/manage/manage-hyper-v-minroot-2016)
	hostCount := int32(processorTopology.LogicalProcessorCount)
	if requested > hostCount {
		log.G(ctx).WithFields(logrus.Fields{
			logfields.UVMID: uvm.id,
			"requested":     requested,
			"assigned":      hostCount,
		}).Warn("Changing user requested CPUCount to current number of processors")
		return hostCount
	} else {
		return requested
	}
}

// ProcessorCount returns the number of processors actually assigned to the UVM.
func (uvm *UtilityVM) ProcessorCount() int32 {
	return uvm.processorCount
}

// PhysicallyBacked returns if the UVM is backed by physical memory
// (Over commit and deferred commit both false)
func (uvm *UtilityVM) PhysicallyBacked() bool {
	return uvm.physicallyBacked
}

// ProcessDumpLocation returns the location that process dumps will get written to for containers running
// in the UVM.
func (uvm *UtilityVM) ProcessDumpLocation() string {
	return uvm.processDumpLocation
}

func (uvm *UtilityVM) normalizeMemorySize(ctx context.Context, requested uint64) uint64 {
	actual := (requested + 1) &^ 1 // align up to an even number
	if requested != actual {
		log.G(ctx).WithFields(logrus.Fields{
			logfields.UVMID: uvm.id,
			"requested":     requested,
			"assigned":      actual,
		}).Warn("Changing user requested MemorySizeInMB to align to 2MB")
	}
	return actual
}

// DevicesPhysicallyBacked describes if additional devices added to the UVM
// should be physically backed
func (uvm *UtilityVM) DevicesPhysicallyBacked() bool {
	return uvm.devicesPhysicallyBacked
}

// VSMBNoDirectMap returns if VSMB devices should be mounted with `NoDirectMap` set to true
func (uvm *UtilityVM) VSMBNoDirectMap() bool {
	return uvm.vsmbNoDirectMap
}

// Closes the external GCS connection if it is being used and also closes the
// listener for GCS connection.
func (uvm *UtilityVM) CloseGCSConnection() (err error) {
	if uvm.gc != nil {
		err = uvm.gc.Close()
	}
	if uvm.gcListener != nil {
		err = uvm.gcListener.Close()
	}
	return
}
