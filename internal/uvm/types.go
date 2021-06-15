package uvm

// This package describes the external interface for utility VMs.

import (
	"net"
	"sync"

	"github.com/Microsoft/hcsshim/internal/gcs"
	"github.com/Microsoft/hcsshim/internal/hcs/schema1"
	"github.com/Microsoft/hcsshim/internal/hns"
	"github.com/Microsoft/hcsshim/internal/ncproxyttrpc"
	"github.com/Microsoft/hcsshim/internal/vm"
)

//                    | WCOW | LCOW
// Container scratch  | SCSI | SCSI
// Scratch space      | ---- | SCSI   // For file system utilities. /tmp/scratch
// Read-Only Layer    | VSMB | VPMEM
// Mapped Directory   | VSMB | PLAN9

// vpmemInfo is an internal structure used for determining VPMem devices mapped to
// a Linux utility VM.
type vpmemInfo struct {
	hostPath string
	uvmPath  string
	refCount uint32
}

type nicInfo struct {
	ID       string
	Endpoint *hns.HNSEndpoint
}

type namespaceInfo struct {
	nics map[string]*nicInfo
}

// UtilityVM is the object used by clients representing a utility VM
type UtilityVM struct {
	builder          vm.UVMBuilder        // Object used to construct the Utility VM configuration document
	vm               vm.UVM               // Underlying interface to the virtstack used to launch the UVM.
	id               string               // Identifier for the utility VM (user supplied or generated)
	owner            string               // Owner for the utility VM (user supplied or generated)
	operatingSystem  string               // "windows" or "linux"
	gcListener       net.Listener         // The GCS connection listener
	gc               *gcs.GuestConnection // The GCS connection
	processorCount   int32
	physicallyBacked bool       // If the uvm is backed by physical memory and not virtual memory
	m                sync.Mutex // Lock for adding/removing devices

	exitErr error
	exitCh  chan struct{}

	// devicesPhysicallyBacked indicates if additional devices added to a uvm should be
	// entirely physically backed
	devicesPhysicallyBacked bool

	// GCS bridge protocol and capabilities
	protocol  uint32
	guestCaps schema1.GuestDefinedCapabilities

	// containerCounter is the current number of containers that have been
	// created. This is never decremented in the life of the UVM.
	//
	// NOTE: All accesses to this MUST be done atomically.
	containerCounter uint64

	// VSMB shares that are mapped into a Windows UVM. These are used for read-only
	// layers and mapped directories.
	// We maintain two sets of maps, `vsmbDirShares` tracks shares that are
	// unrestricted mappings of directories. `vsmbFileShares` tracks shares that
	// are restricted to some subset of files in the directory. This is used as
	// part of a temporary fix to allow WCOW single-file mapping to function.
	vsmbDirShares   map[string]*VSMBShare
	vsmbFileShares  map[string]*VSMBShare
	vsmbCounter     uint64 // Counter to generate a unique share name for each VSMB share.
	vsmbNoDirectMap bool   // indicates if VSMB devices should be added with the `NoDirectMap` option

	// VPMEM devices that are mapped into a Linux UVM. These are used for read-only layers, or for
	// booting from VHD.
	vpmemDevices      [MaxVPMEMCount]*vpmemInfo // Limited by ACPI size.
	vpmemMaxCount     uint32                    // The max number of VPMem devices.
	vpmemMaxSizeBytes uint64                    // The max size of the layer in bytes per vPMem device.

	// SCSI devices that are mapped into a Windows or Linux utility VM
	scsiLocations       [4][64]*SCSIMount // Hyper-V supports 4 controllers, 64 slots per controller. Limited to 1 controller for now though.
	scsiControllerCount uint32            // Number of SCSI controllers in the utility VM

	vpciDevices map[string]*VPCIDevice // map of device instance id to vpci device

	// Plan9 are directories mapped into a Linux utility VM
	plan9Counter uint64 // Each newly-added plan9 share has a counter used as its ID in the ResourceURI and for the name

	namespaces map[string]*namespaceInfo

	outputListener       net.Listener
	outputProcessingDone chan struct{}
	outputHandler        OutputHandler

	entropyListener net.Listener

	// mountCounter is the number of mounts that have been added to the UVM
	// This is used in generating a unique mount path inside the UVM for every mount.
	// Access to this variable should be done atomically.
	mountCounter uint64

	// specifies if this UVM is created to be saved as a template
	IsTemplate bool

	// specifies if this UVM is a cloned from a template
	IsClone bool

	// ID of the template from which this clone was created. Only applies when IsClone
	// is true
	TemplateID string

	// The CreateOpts used to create this uvm. These can be either of type
	// uvm.OptionsLCOW or uvm.OptionsWCOW
	createOpts interface{}
	// Network config proxy client. If nil then this wasn't requested and the
	// uvms network will be configured locally.
	ncProxyClient ncproxyttrpc.NetworkConfigProxyService

	// networkSetup handles the logic for setting up and tearing down any network configuration
	// for the Utility VM.
	networkSetup NetworkSetup
}
