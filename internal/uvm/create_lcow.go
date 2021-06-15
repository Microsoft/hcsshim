package uvm

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/Microsoft/hcsshim/internal/ncproxyttrpc"
	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/Microsoft/hcsshim/internal/vm/hcs"
	"github.com/Microsoft/hcsshim/internal/vm/remotevm"
	"github.com/containerd/ttrpc"

	"github.com/Microsoft/go-winio"
	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/internal/gcs"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/logfields"
	"github.com/Microsoft/hcsshim/internal/oc"
	"github.com/Microsoft/hcsshim/internal/processorinfo"
	"github.com/Microsoft/hcsshim/osversion"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"go.opencensus.io/trace"
)

type PreferredRootFSType int

const (
	PreferredRootFSTypeInitRd PreferredRootFSType = iota
	PreferredRootFSTypeVHD
	entropyVsockPort  = 1
	linuxLogVsockPort = 109
)

// OutputHandler is used to process the output from the program run in the UVM.
type OutputHandler func(io.Reader)

const (
	// InitrdFile is the default file name for an initrd.img used to boot LCOW.
	InitrdFile = "initrd.img"
	// VhdFile is the default file name for a rootfs.vhd used to boot LCOW.
	VhdFile = "rootfs.vhd"
	// KernelFile is the default file name for a kernel used to boot LCOW.
	KernelFile = "kernel"
	// UncompressedKernelFile is the default file name for an uncompressed
	// kernel used to boot LCOW with KernelDirect.
	UncompressedKernelFile = "vmlinux"
)

// OptionsLCOW are the set of options passed to CreateLCOW() to create a utility vm.
type OptionsLCOW struct {
	*Options

	BootFilesPath         string              // Folder in which kernel and root file system reside. Defaults to \Program Files\Linux Containers
	KernelFile            string              // Filename under `BootFilesPath` for the kernel. Defaults to `kernel`
	KernelDirect          bool                // Skip UEFI and boot directly to `kernel`
	RootFSFile            string              // Filename under `BootFilesPath` for the UVMs root file system. Defaults to `InitrdFile`
	KernelBootOptions     string              // Additional boot options for the kernel
	EnableGraphicsConsole bool                // If true, enable a graphics console for the utility VM
	ConsolePipe           string              // The named pipe path to use for the serial console.  eg \\.\pipe\vmpipe
	SCSIControllerCount   uint32              // The number of SCSI controllers. Defaults to 1. Currently we only support 0 or 1.
	UseGuestConnection    bool                // Whether the HCS should connect to the UVM's GCS. Defaults to true
	ExecCommandLine       string              // The command line to exec from init. Defaults to GCS
	ForwardStdout         bool                // Whether stdout will be forwarded from the executed program. Defaults to false
	ForwardStderr         bool                // Whether stderr will be forwarded from the executed program. Defaults to true
	OutputHandler         OutputHandler       `json:"-"` // Controls how output received over HVSocket from the UVM is handled. Defaults to parsing output as logrus messages
	VPMemDeviceCount      uint32              // Number of VPMem devices. Defaults to `DefaultVPMEMCount`. Limit at 128. If booting UVM from VHD, device 0 is taken.
	VPMemSizeBytes        uint64              // Size of the VPMem devices. Defaults to `DefaultVPMemSizeBytes`.
	PreferredRootFSType   PreferredRootFSType // If `KernelFile` is `InitrdFile` use `PreferredRootFSTypeInitRd`. If `KernelFile` is `VhdFile` use `PreferredRootFSTypeVHD`
	EnableColdDiscardHint bool                // Whether the HCS should use cold discard hints. Defaults to false
	VPCIEnabled           bool                // Whether the kernel should enable pci
}

// defaultLCOWOSBootFilesPath returns the default path used to locate the LCOW
// OS kernel and root FS files. This default is the subdirectory
// `LinuxBootFiles` in the directory of the executable that started the current
// process; or, if it does not exist, `%ProgramFiles%\Linux Containers`.
func defaultLCOWOSBootFilesPath() string {
	localDirPath := filepath.Join(filepath.Dir(os.Args[0]), "LinuxBootFiles")
	if _, err := os.Stat(localDirPath); err == nil {
		return localDirPath
	}
	return filepath.Join(os.Getenv("ProgramFiles"), "Linux Containers")
}

// NewDefaultOptionsLCOW creates the default options for a bootable version of
// LCOW.
//
// `id` the ID of the compute system. If not passed will generate a new GUID.
//
// `owner` the owner of the compute system. If not passed will use the
// executable files name.
func NewDefaultOptionsLCOW(id, owner string) *OptionsLCOW {
	// Use KernelDirect boot by default on all builds that support it.
	kernelDirectSupported := osversion.Build() >= 18286
	opts := &OptionsLCOW{
		Options:               newDefaultOptions(id, owner),
		BootFilesPath:         defaultLCOWOSBootFilesPath(),
		KernelFile:            KernelFile,
		KernelDirect:          kernelDirectSupported,
		RootFSFile:            InitrdFile,
		KernelBootOptions:     "",
		EnableGraphicsConsole: false,
		ConsolePipe:           "",
		SCSIControllerCount:   1,
		UseGuestConnection:    true,
		ExecCommandLine:       fmt.Sprintf("/bin/gcs -v4 -log-format json -loglevel %s", logrus.StandardLogger().Level.String()),
		ForwardStdout:         false,
		ForwardStderr:         true,
		OutputHandler:         parseLogrus(id),
		VPMemDeviceCount:      DefaultVPMEMCount,
		VPMemSizeBytes:        DefaultVPMemSizeBytes,
		PreferredRootFSType:   PreferredRootFSTypeInitRd,
		EnableColdDiscardHint: false,
		VPCIEnabled:           false,
	}

	if _, err := os.Stat(filepath.Join(opts.BootFilesPath, VhdFile)); err == nil {
		// We have a rootfs.vhd in the boot files path. Use it over an initrd.img
		opts.RootFSFile = VhdFile
		opts.PreferredRootFSType = PreferredRootFSTypeVHD
	}

	if kernelDirectSupported {
		// KernelDirect supports uncompressed kernel if the kernel is present.
		// Default to uncompressed if on box. NOTE: If `kernel` is already
		// uncompressed and simply named 'kernel' it will still be used
		// uncompressed automatically.
		if _, err := os.Stat(filepath.Join(opts.BootFilesPath, UncompressedKernelFile)); err == nil {
			opts.KernelFile = UncompressedKernelFile
		}
	}
	return opts
}

// CreateLCOW creates a Linux Utility VM.
func CreateLCOW(ctx context.Context, opts *OptionsLCOW) (_ *UtilityVM, err error) {
	ctx, span := trace.StartSpan(ctx, "uvm::CreateLCOW")
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
	log.G(ctx).WithField("options", fmt.Sprintf("%+v", opts)).Debug("uvm::CreateLCOW options")

	// We dont serialize OutputHandler so if it is missing we need to put it back to the default.
	if opts.OutputHandler == nil {
		opts.OutputHandler = parseLogrus(opts.ID)
	}

	uvm := &UtilityVM{
		id:                      opts.ID,
		owner:                   opts.Owner,
		operatingSystem:         "linux",
		scsiControllerCount:     opts.SCSIControllerCount,
		vpmemMaxCount:           opts.VPMemDeviceCount,
		vpmemMaxSizeBytes:       opts.VPMemSizeBytes,
		vpciDevices:             make(map[string]*VPCIDevice),
		physicallyBacked:        !opts.AllowOvercommit,
		devicesPhysicallyBacked: opts.FullyPhysicallyBacked,
		createOpts:              opts,
	}

	defer func() {
		if err != nil {
			uvm.Close()
		}
	}()

	kernelFullPath := filepath.Join(opts.BootFilesPath, opts.KernelFile)
	if _, err := os.Stat(kernelFullPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("kernel: '%s' not found", kernelFullPath)
	}

	rootfsFullPath := filepath.Join(opts.BootFilesPath, opts.RootFSFile)
	if _, err := os.Stat(rootfsFullPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("boot file: '%s' not found", rootfsFullPath)
	}

	if err := verifyOptions(ctx, opts); err != nil {
		return nil, errors.Wrap(err, errBadUVMOpts.Error())
	}

	processorTopology, err := processorinfo.HostProcessorInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get host processor information: %s", err)
	}

	var (
		uvmb  vm.UVMBuilder
		cOpts []vm.CreateOpt
	)
	switch opts.VMSource {
	case vm.HCS:
		uvmb, err = hcs.NewUVMBuilder(uvm.id, uvm.owner, vm.Linux)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create UVM builder")
		}
		cOpts = applyHcsOpts(opts)
	case vm.RemoteVM:
		uvmb, err = remotevm.NewUVMBuilder(ctx, uvm.id, uvm.owner, opts.VMServicePath, opts.VMServiceAddress, vm.Linux)
		if err != nil {
			return nil, errors.Wrap(err, "failed to create UVM builder")
		}
		cOpts = applyRemoteVMOpts(opts.Options)
	default:
		return nil, fmt.Errorf("unknown VM source: %s", opts.VMSource)
	}
	uvm.builder = uvmb

	// To maintain compatability with Docker we need to automatically downgrade
	// a user CPU count if the setting is not possible.
	uvm.processorCount = uvm.normalizeProcessorCount(ctx, opts.ProcessorCount, processorTopology)

	// Align the requested memory size.
	memorySizeInMB := uvm.normalizeMemorySize(ctx, opts.MemorySizeInMB)

	// We can set a cpu group for the VM at creation time in recent builds.
	if opts.CPUGroupID != "" {
		if osversion.Build() < cpuGroupCreateBuild {
			return nil, errCPUGroupCreateNotSupported
		}
		windows, ok := uvmb.(vm.WindowsConfigManager)
		if !ok {
			return nil, errors.Wrap(vm.ErrNotSupported, "stopping cpu group setup")
		}
		if err := windows.SetCPUGroup(ctx, opts.CPUGroupID); err != nil {
			return nil, err
		}
	}

	mem, ok := uvmb.(vm.MemoryManager)
	if !ok {
		return nil, errors.Wrap(vm.ErrNotSupported, "stopping memory setup")
	}

	if err := mem.SetMemoryLimit(ctx, memorySizeInMB); err != nil {
		return nil, errors.Wrap(err, "failed to set memory limit")
	}

	backingType := vm.MemoryBackingTypeVirtual
	if !opts.AllowOvercommit {
		backingType = vm.MemoryBackingTypePhysical
	}

	if err := mem.SetMemoryConfig(&vm.MemoryConfig{
		BackingType:     backingType,
		DeferredCommit:  opts.EnableDeferredCommit,
		ColdDiscardHint: opts.EnableColdDiscardHint,
		HotHint:         opts.AllowOvercommit,
	}); err != nil {
		return nil, errors.Wrap(err, "failed to set memory config")
	}

	proc, ok := uvmb.(vm.ProcessorManager)
	if !ok {
		return nil, errors.Wrap(vm.ErrNotSupported, "stopping processor setup")
	}

	if err := proc.SetProcessorCount(uint32(uvm.processorCount)); err != nil {
		return nil, errors.Wrap(err, "failed to set processor count")
	}

	// Handle StorageQoS if set
	if opts.StorageQoSBandwidthMaximum > 0 || opts.StorageQoSIopsMaximum > 0 {
		storage, ok := uvmb.(vm.StorageQosManager)
		if !ok {
			return nil, errors.Wrap(vm.ErrNotSupported, "stopping storageqos setup")
		}
		if err := storage.SetStorageQos(int64(opts.StorageQoSIopsMaximum), int64(opts.StorageQoSBandwidthMaximum)); err != nil {
			return nil, errors.Wrap(err, "failed to set storage qos config")
		}
	}

	if uvm.scsiControllerCount > 0 {
		scsi, ok := uvmb.(vm.SCSIManager)
		if !ok {
			return nil, errors.Wrap(vm.ErrNotSupported, "stopping SCSI setup")
		}
		for i := 0; i < int(uvm.scsiControllerCount); i++ {
			if err := scsi.AddSCSIController(uint32(i)); err != nil {
				return nil, errors.Wrap(err, "failed to add scsi controller")
			}
		}
	}

	if uvm.vpmemMaxCount > 0 {
		vpmem, ok := uvmb.(vm.VPMemManager)
		if !ok {
			return nil, errors.Wrap(vm.ErrNotSupported, "stopping VPMem setup")
		}
		if err := vpmem.AddVPMemController(uvm.vpmemMaxCount, uvm.vpmemMaxSizeBytes); err != nil {
			return nil, errors.Wrap(err, "failed to add VPMem controller")
		}
	}

	var kernelArgs string
	switch opts.PreferredRootFSType {
	case PreferredRootFSTypeInitRd:
		if !opts.KernelDirect {
			kernelArgs = "initrd=/" + opts.RootFSFile
		}
	case PreferredRootFSTypeVHD:
		// Support for VPMem VHD(X) booting rather than initrd..
		kernelArgs = "root=/dev/pmem0 ro rootwait init=/init"
		imageFormat := vm.VPMemImageFormatVHD1
		if strings.ToLower(filepath.Ext(opts.RootFSFile)) == "vhdx" {
			imageFormat = vm.VPMemImageFormatVHDX
		}

		vpmem, ok := uvmb.(vm.VPMemManager)
		if !ok {
			return nil, errors.Wrap(vm.ErrNotSupported, "stopping VPMem setup")
		}
		if err := vpmem.AddVPMemDevice(ctx, 0, rootfsFullPath, true, imageFormat); err != nil {
			return nil, errors.Wrap(err, "failed to add vpmem disk")
		}

		// Add to our internal structure
		uvm.vpmemDevices[0] = &vpmemInfo{
			hostPath: opts.RootFSFile,
			uvmPath:  "/",
			refCount: 1,
		}
	}

	vmDebugging := false
	if opts.ConsolePipe != "" {
		vmDebugging = true
		kernelArgs += " 8250_core.nr_uarts=1 8250_core.skip_txen_test=1 console=ttyS0,115200"

		serial, ok := uvm.vm.(vm.SerialManager)
		if !ok {
			return nil, errors.Wrap(vm.ErrNotSupported, "stopping serial console setup")
		}

		if err := serial.SetSerialConsole(0, opts.ConsolePipe); err != nil {
			return nil, errors.Wrap(err, "failed to add serial console config")
		}
	} else {
		kernelArgs += " 8250_core.nr_uarts=0"
	}

	if !vmDebugging {
		// Terminate the VM if there is a kernel panic.
		kernelArgs += " panic=-1 quiet"
	}

	if opts.KernelBootOptions != "" {
		kernelArgs += " " + opts.KernelBootOptions
	}

	if !opts.VPCIEnabled {
		kernelArgs += ` pci=off`
	}

	// Inject initial entropy over vsock during init launch.
	initArgs := fmt.Sprintf("-e %d", entropyVsockPort)

	// With default options, run GCS with stderr pointing to the vsock port
	// created below in order to forward guest logs to logrus.
	initArgs += " /bin/vsockexec"

	if opts.ForwardStdout {
		initArgs += fmt.Sprintf(" -o %d", linuxLogVsockPort)
	}

	if opts.ForwardStderr {
		initArgs += fmt.Sprintf(" -e %d", linuxLogVsockPort)
	}

	initArgs += " " + opts.ExecCommandLine

	if vmDebugging {
		// Launch a shell on the console.
		initArgs = `sh -c "` + initArgs + ` & exec sh"`
	}

	kernelArgs += fmt.Sprintf(" nr_cpus=%d", opts.ProcessorCount)
	kernelArgs += ` brd.rd_nr=0 pmtmr=0 -- ` + initArgs

	boot, ok := uvmb.(vm.BootManager)
	if !ok {
		return nil, errors.Wrap(vm.ErrNotSupported, "stopping boot configuration")
	}
	if opts.KernelDirect {
		var initFS string
		if opts.PreferredRootFSType == PreferredRootFSTypeInitRd {
			initFS = rootfsFullPath
		}
		if err := boot.SetLinuxKernelDirectBoot(kernelFullPath, initFS, kernelArgs); err != nil {
			return nil, errors.Wrap(err, "failed to set Linux kernel direct boot")
		}
	} else {
		if err := boot.SetUEFIBoot(opts.BootFilesPath, opts.KernelFile, kernelArgs); err != nil {
			return nil, errors.Wrap(err, "failed to set UEFI boot")
		}
	}

	uvm.vm, err = uvmb.Create(ctx, cOpts)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create virtual machine")
	}

	// Cerate a socket to inject entropy during boot.
	uvm.entropyListener, err = uvm.listenVsock(ctx, entropyVsockPort)
	if err != nil {
		return nil, err
	}

	// Create a socket that the executed program can send to. This is usually
	// used by GCS to send log data.
	if opts.ForwardStdout || opts.ForwardStderr {
		uvm.outputHandler = opts.OutputHandler
		uvm.outputProcessingDone = make(chan struct{})
		uvm.outputListener, err = uvm.listenVsock(ctx, linuxLogVsockPort)
		if err != nil {
			return nil, err
		}
	}

	if opts.UseGuestConnection {
		log.G(ctx).WithField("vmID", uvm.vm.VmID()).Debug("Using external GCS bridge")
		l, err := uvm.listenVsock(ctx, gcs.LinuxGcsVsockPort)
		if err != nil {
			return nil, err
		}
		uvm.gcListener = l
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

func (uvm *UtilityVM) listenVsock(ctx context.Context, port uint32) (net.Listener, error) {
	vmsocket, ok := uvm.vm.(vm.VMSocketManager)
	if !ok {
		return nil, errors.Wrap(vm.ErrNotSupported, "stopping vm socket configuration")
	}
	return vmsocket.VMSocketListen(ctx, vm.HvSocket, winio.VsockServiceID(port))
}
