package uvm

// This package describes the external interface for utility VMs.

import (
	"net"
	"sync"

	"github.com/Microsoft/go-winio/pkg/guid"
	"golang.org/x/sys/windows"

	"github.com/Microsoft/hcsshim/internal/gcs"
	"github.com/Microsoft/hcsshim/internal/hcs"
	"github.com/Microsoft/hcsshim/internal/hcs/schema1"
	"github.com/Microsoft/hcsshim/internal/hns"
	"github.com/Microsoft/hcsshim/internal/ncproxyttrpc"
)

//                    | WCOW | LCOW
// Container scratch  | SCSI | SCSI
// Scratch space      | ---- | SCSI   // For file system utilities. /tmp/scratch
// Read-Only Layer    | VSMB | VPMEM
// Mapped Directory   | VSMB | PLAN9

type nicInfo struct {
	ID       string
	Endpoint *hns.HNSEndpoint
}

type namespaceInfo struct {
	nics map[string]*nicInfo
}

// UtilityVM is the object used by clients representing a utility VM
type UtilityVM struct {
	id               string               // Identifier for the utility VM (user supplied or generated)
	runtimeID        guid.GUID            // Hyper-V VM ID
	owner            string               // Owner for the utility VM (user supplied or generated)
	operatingSystem  string               // "windows" or "linux"
	hcsSystem        *hcs.System          // The handle to the compute system
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
	vpmemMaxCount           uint32 // The max number of VPMem devices.
	vpmemMaxSizeBytes       uint64 // The max size of the layer in bytes per vPMem device.
	vpmemMultiMapping       bool   // Enable mapping multiple VHDs onto a single VPMem device
	vpmemDevicesDefault     [MaxVPMEMCount]*vPMemInfoDefault
	vpmemDevicesMultiMapped [MaxVPMEMCount]*vPMemInfoMulti

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

	// Handle to the vmmem process associated with this UVM. Used to look up
	// memory metrics for the UVM.
	vmmemProcess windows.Handle
	// Tracks the error returned when looking up the vmmem process.
	vmmemErr error
	// We only need to look up the vmmem process once, then we keep a handle
	// open.
	vmmemOnce sync.Once

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

	// Location that container process dumps will get written too.
	processDumpLocation string

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
