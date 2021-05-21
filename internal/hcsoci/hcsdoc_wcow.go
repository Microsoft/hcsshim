// +build windows

package hcsoci

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Microsoft/hcsshim/internal/hcs/schema1"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/layers"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/oci"
	"github.com/Microsoft/hcsshim/internal/processorinfo"
	"github.com/Microsoft/hcsshim/internal/uvm"
	"github.com/Microsoft/hcsshim/internal/uvmfolder"
	"github.com/Microsoft/hcsshim/internal/wclayer"
	"github.com/Microsoft/hcsshim/osversion"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

// A simple wrapper struct around the container mount configs that should be added to the
// container.
type mountsConfig struct {
	mdsv1 []schema1.MappedDir
	mpsv1 []schema1.MappedPipe
	mdsv2 []hcsschema.MappedDirectory
	mpsv2 []hcsschema.MappedPipe
}

func createMountsConfig(ctx context.Context, coi *createOptionsInternal) (*mountsConfig, error) {
	// Add the mounts as mapped directories or mapped pipes
	// TODO: Mapped pipes to add in v2 schema.
	var config mountsConfig
	for _, mount := range coi.Spec.Mounts {

		if uvm.IsPipe(mount.Source) {
			src, dst := uvm.GetContainerPipeMapping(coi.HostingSystem, mount)
			config.mpsv1 = append(config.mpsv1, schema1.MappedPipe{HostPath: src, ContainerPipeName: dst})
			config.mpsv2 = append(config.mpsv2, hcsschema.MappedPipe{HostPath: src, ContainerPipeName: dst})
		} else {
			readOnly := false
			for _, o := range mount.Options {
				if strings.ToLower(o) == "ro" {
					readOnly = true
				}
			}
			mdv1 := schema1.MappedDir{HostPath: mount.Source, ContainerPath: mount.Destination, ReadOnly: readOnly}
			mdv2 := hcsschema.MappedDirectory{ContainerPath: mount.Destination, ReadOnly: readOnly}
			if coi.HostingSystem == nil {
				// HCS has a bug where it does not correctly resolve file (not dir) paths
				// if the path includes a symlink. Therefore, we resolve the path here before
				// passing it in. The issue does not occur with VSMB, so don't need to worry
				// about the isolated case.
				src, err := filepath.EvalSymlinks(mount.Source)
				if err != nil {
					return nil, fmt.Errorf("failed to eval symlinks for mount source %q: %s", mount.Source, err)
				}
				mdv2.HostPath = src
			} else if mount.Type == "virtual-disk" || mount.Type == "physical-disk" {
				uvmPath, err := coi.HostingSystem.GetScsiUvmPath(ctx, mount.Source)
				if err != nil {
					return nil, err
				}
				mdv2.HostPath = uvmPath
			} else {
				// vsmb mount
				uvmPath, err := coi.HostingSystem.GetVSMBUvmPath(ctx, mount.Source, readOnly)
				if err != nil {
					return nil, err
				}
				mdv2.HostPath = uvmPath
			}
			config.mdsv1 = append(config.mdsv1, mdv1)
			config.mdsv2 = append(config.mdsv2, mdv2)
		}
	}
	return &config, nil
}

// ConvertCPULimits handles the logic of converting and validating the containers CPU limits
// specified in the OCI spec to what HCS expects.
//
// `cid` is the container's ID.
//
// `vmid` is the Utility VM's ID if the container we're constructing is going to belong to
// one.
//
// `spec` is the OCI spec for the container.
//
// `maxCPUCount` is the maximum cpu count allowed for the container. This value should
// be the number of processors on the host, or in the case of a hypervisor isolated container
// the number of processors assigned to the guest/Utility VM.
//
// Returns the cpu count, cpu limit, and cpu weight in this order. Returns an error if more than one of
// cpu count, cpu limit, or cpu weight was specified in the OCI spec as they are mutually
// exclusive.
func ConvertCPULimits(ctx context.Context, cid string, spec *specs.Spec, maxCPUCount int32) (int32, int32, int32, error) {
	cpuNumSet := 0
	cpuCount := oci.ParseAnnotationsCPUCount(ctx, spec, oci.AnnotationContainerProcessorCount, 0)
	if cpuCount > 0 {
		cpuNumSet++
	}

	cpuLimit := oci.ParseAnnotationsCPULimit(ctx, spec, oci.AnnotationContainerProcessorLimit, 0)
	if cpuLimit > 0 {
		cpuNumSet++
	}

	cpuWeight := oci.ParseAnnotationsCPUWeight(ctx, spec, oci.AnnotationContainerProcessorWeight, 0)
	if cpuWeight > 0 {
		cpuNumSet++
	}

	if cpuNumSet > 1 {
		return 0, 0, 0, fmt.Errorf("invalid spec - Windows Container CPU Count: '%d', Limit: '%d', and Weight: '%d' are mutually exclusive", cpuCount, cpuLimit, cpuWeight)
	} else if cpuNumSet == 1 {
		cpuCount = NormalizeProcessorCount(ctx, cid, cpuCount, maxCPUCount)
	}
	return cpuCount, cpuLimit, cpuWeight, nil
}

// createWindowsContainerDocument creates documents for passing to HCS or GCS to create
// a container, both hosted and process isolated. It creates both v1 and v2
// container objects, WCOW only. The containers storage should have been mounted already.
func createWindowsContainerDocument(ctx context.Context, coi *createOptionsInternal) (*schema1.ContainerConfig, *hcsschema.Container, error) {
	log.G(ctx).Debug("hcsshim: CreateHCSContainerDocument")
	// TODO: Make this safe if exported so no null pointer dereferences.

	if coi.Spec == nil {
		return nil, nil, fmt.Errorf("cannot create HCS container document - OCI spec is missing")
	}

	if coi.Spec.Windows == nil {
		return nil, nil, fmt.Errorf("cannot create HCS container document - OCI spec Windows section is missing ")
	}

	v1 := &schema1.ContainerConfig{
		SystemType:              "Container",
		Name:                    coi.actualID,
		Owner:                   coi.actualOwner,
		HvPartition:             false,
		IgnoreFlushesDuringBoot: coi.Spec.Windows.IgnoreFlushesDuringBoot,
	}

	// IgnoreFlushesDuringBoot is a property of the SCSI attachment for the scratch. Set when it's hot-added to the utility VM
	// ID is a property on the create call in V2 rather than part of the schema.
	v2Container := &hcsschema.Container{Storage: &hcsschema.Storage{}}

	// TODO: Still want to revisit this.
	if coi.Spec.Windows.LayerFolders == nil || len(coi.Spec.Windows.LayerFolders) < 2 {
		return nil, nil, fmt.Errorf("invalid spec - not enough layer folders supplied")
	}

	if coi.Spec.Hostname != "" {
		v1.HostName = coi.Spec.Hostname
		v2Container.GuestOs = &hcsschema.GuestOs{HostName: coi.Spec.Hostname}
	}

	var (
		uvmCPUCount  int32
		hostCPUCount = processorinfo.ProcessorCount()
		maxCPUCount  = hostCPUCount
	)

	if coi.HostingSystem != nil {
		uvmCPUCount = coi.HostingSystem.ProcessorCount()
		maxCPUCount = uvmCPUCount
	}

	cpuCount, cpuLimit, cpuWeight, err := ConvertCPULimits(ctx, coi.ID, coi.Spec, maxCPUCount)
	if err != nil {
		return nil, nil, err
	}

	if coi.HostingSystem != nil && coi.ScaleCPULimitsToSandbox && cpuLimit > 0 {
		// When ScaleCPULimitsToSandbox is set and we are running in a UVM, we assume
		// the CPU limit has been calculated based on the number of processors on the
		// host, and instead re-calculate it based on the number of processors in the UVM.
		//
		// This is needed to work correctly with assumptions kubelet makes when computing
		// the CPU limit value:
		// - kubelet thinks about CPU limits in terms of millicores, which are 1000ths of
		//   cores. So if 2000 millicores are assigned, the container can use 2 processors.
		// - In Windows, the job object CPU limit is global across all processors on the
		//   system, and is represented as a fraction out of 10000. In this model, a limit
		//   of 10000 means the container can use all processors fully, regardless of how
		//   many processors exist on the system.
		// - To convert the millicores value into the job object limit, kubelet divides
		//   the millicores by the number of CPU cores on the host. This causes problems
		//   when running inside a UVM, as the UVM may have a different number of processors
		//   than the host system.
		//
		// To work around this, we undo the division by the number of host processors, and
		// re-do the division based on the number of processors inside the UVM. This will
		// give the correct value based on the actual number of millicores that the kubelet
		// wants the container to have.
		//
		// Kubelet formula to compute CPU limit:
		// cpuMaximum := 10000 * cpuLimit.MilliValue() / int64(runtime.NumCPU()) / 1000
		newCPULimit := cpuLimit * hostCPUCount / uvmCPUCount
		// We only apply bounds here because we are calculating the CPU limit ourselves,
		// and this matches the kubelet behavior where they also bound the CPU limit by [1, 10000].
		// In the case where we use the value directly from the user, we don't alter it to fit
		// within the bounds, but just let the platform throw an error if it is invalid.
		if newCPULimit < 1 {
			newCPULimit = 1
		} else if newCPULimit > 10000 {
			newCPULimit = 10000
		}
		log.G(ctx).WithFields(logrus.Fields{
			"hostCPUCount": hostCPUCount,
			"uvmCPUCount":  uvmCPUCount,
			"oldCPULimit":  cpuLimit,
			"newCPULimit":  newCPULimit,
		}).Info("rescaling CPU limit for UVM sandbox")
		cpuLimit = newCPULimit
	}

	v1.ProcessorCount = uint32(cpuCount)
	v1.ProcessorMaximum = int64(cpuLimit)
	v1.ProcessorWeight = uint64(cpuWeight)

	v2Container.Processor = &hcsschema.Processor{
		Count:   cpuCount,
		Maximum: cpuLimit,
		Weight:  cpuWeight,
	}

	// Memory Resources
	memoryMaxInMB := oci.ParseAnnotationsMemory(ctx, coi.Spec, oci.AnnotationContainerMemorySizeInMB, 0)
	if memoryMaxInMB > 0 {
		v1.MemoryMaximumInMB = int64(memoryMaxInMB)
		v2Container.Memory = &hcsschema.Memory{
			SizeInMB: memoryMaxInMB,
		}
	}

	// Storage Resources
	storageBandwidthMax := oci.ParseAnnotationsStorageBps(ctx, coi.Spec, oci.AnnotationContainerStorageQoSBandwidthMaximum, 0)
	storageIopsMax := oci.ParseAnnotationsStorageIops(ctx, coi.Spec, oci.AnnotationContainerStorageQoSIopsMaximum, 0)
	if storageBandwidthMax > 0 || storageIopsMax > 0 {
		v1.StorageBandwidthMaximum = uint64(storageBandwidthMax)
		v1.StorageIOPSMaximum = uint64(storageIopsMax)
		v2Container.Storage.QoS = &hcsschema.StorageQoS{
			BandwidthMaximum: storageBandwidthMax,
			IopsMaximum:      storageIopsMax,
		}
	}

	// TODO V2 networking. Only partial at the moment. v2.Container.Networking.Namespace specifically
	if coi.Spec.Windows.Network != nil {
		v2Container.Networking = &hcsschema.Networking{}

		v1.EndpointList = coi.Spec.Windows.Network.EndpointList

		// Use the reserved network namespace for containers created inside
		// cloned or template UVMs.
		if coi.HostingSystem != nil && (coi.HostingSystem.IsTemplate || coi.HostingSystem.IsClone) {
			v2Container.Networking.Namespace = uvm.DEFAULT_CLONE_NETWORK_NAMESPACE_ID
		} else {
			v2Container.Networking.Namespace = coi.actualNetworkNamespace
		}

		v1.AllowUnqualifiedDNSQuery = coi.Spec.Windows.Network.AllowUnqualifiedDNSQuery
		v2Container.Networking.AllowUnqualifiedDnsQuery = v1.AllowUnqualifiedDNSQuery

		if coi.Spec.Windows.Network.DNSSearchList != nil {
			v1.DNSSearchList = strings.Join(coi.Spec.Windows.Network.DNSSearchList, ",")
			v2Container.Networking.DnsSearchList = v1.DNSSearchList
		}

		v1.NetworkSharedContainerName = coi.Spec.Windows.Network.NetworkSharedContainerName
		v2Container.Networking.NetworkSharedContainerName = v1.NetworkSharedContainerName
	}

	if cs, ok := coi.Spec.Windows.CredentialSpec.(string); ok {
		v1.Credentials = cs
		// If this is a HCS v2 schema container, we created the CCG instance
		// with the other container resources. Pass the CCG state information
		// as part of the container document.
		if coi.ccgState != nil {
			v2Container.ContainerCredentialGuard = coi.ccgState
		}
	}

	if coi.Spec.Root == nil {
		return nil, nil, fmt.Errorf("spec is invalid - root isn't populated")
	}

	if coi.Spec.Root.Readonly {
		return nil, nil, fmt.Errorf(`invalid container spec - readonly is not supported for Windows containers`)
	}

	// Strip off the top-most RW/scratch layer as that's passed in separately to HCS for v1
	v1.LayerFolderPath = coi.Spec.Windows.LayerFolders[len(coi.Spec.Windows.LayerFolders)-1]

	if coi.isV2Argon() || coi.isV1Argon() {
		// Argon v1 or v2.
		const volumeGUIDRegex = `^\\\\\?\\(Volume)\{{0,1}[0-9a-fA-F]{8}\-[0-9a-fA-F]{4}\-[0-9a-fA-F]{4}\-[0-9a-fA-F]{4}\-[0-9a-fA-F]{12}(\}){0,1}\}(|\\)$`
		if matched, err := regexp.MatchString(volumeGUIDRegex, coi.Spec.Root.Path); !matched || err != nil {
			return nil, nil, fmt.Errorf(`invalid container spec - Root.Path '%s' must be a volume GUID path in the format '\\?\Volume{GUID}\'`, coi.Spec.Root.Path)
		}
		if coi.Spec.Root.Path[len(coi.Spec.Root.Path)-1] != '\\' {
			coi.Spec.Root.Path += `\` // Be nice to clients and make sure well-formed for back-compat
		}
		v1.VolumePath = coi.Spec.Root.Path[:len(coi.Spec.Root.Path)-1] // Strip the trailing backslash. Required for v1.
		v2Container.Storage.Path = coi.Spec.Root.Path
	} else if coi.isV1Xenon() {
		// V1 Xenon
		v1.HvPartition = true
		if coi.Spec == nil || coi.Spec.Windows == nil || coi.Spec.Windows.HyperV == nil { // Be resilient to nil de-reference
			return nil, nil, fmt.Errorf(`invalid container spec - Spec.Windows.HyperV is nil`)
		}
		if coi.Spec.Windows.HyperV.UtilityVMPath != "" {
			// Client-supplied utility VM path
			v1.HvRuntime = &schema1.HvRuntime{ImagePath: coi.Spec.Windows.HyperV.UtilityVMPath}
		} else {
			// Client was lazy. Let's locate it from the layer folders instead.
			uvmImagePath, err := uvmfolder.LocateUVMFolder(ctx, coi.Spec.Windows.LayerFolders)
			if err != nil {
				return nil, nil, err
			}
			v1.HvRuntime = &schema1.HvRuntime{ImagePath: filepath.Join(uvmImagePath, `UtilityVM`)}
		}
	} else if coi.isV2Xenon() {
		// Hosting system was supplied, so is v2 Xenon.
		v2Container.Storage.Path = coi.Spec.Root.Path
		if coi.HostingSystem.OS() == "windows" {
			layers, err := layers.GetHCSLayers(ctx, coi.HostingSystem, coi.Spec.Windows.LayerFolders[:len(coi.Spec.Windows.LayerFolders)-1])
			if err != nil {
				return nil, nil, err
			}
			v2Container.Storage.Layers = layers
		}
	}

	if coi.isV2Argon() || coi.isV1Argon() { // Argon v1 or v2
		for _, layerPath := range coi.Spec.Windows.LayerFolders[:len(coi.Spec.Windows.LayerFolders)-1] {
			layerID, err := wclayer.LayerID(ctx, layerPath)
			if err != nil {
				return nil, nil, err
			}
			v1.Layers = append(v1.Layers, schema1.Layer{ID: layerID.String(), Path: layerPath})
			v2Container.Storage.Layers = append(v2Container.Storage.Layers, hcsschema.Layer{Id: layerID.String(), Path: layerPath})
		}
	}

	mounts, err := createMountsConfig(ctx, coi)
	if err != nil {
		return nil, nil, err
	}
	v1.MappedDirectories = mounts.mdsv1
	v2Container.MappedDirectories = mounts.mdsv2
	if len(mounts.mpsv1) > 0 && osversion.Build() < osversion.RS3 {
		return nil, nil, fmt.Errorf("named pipe mounts are not supported on this version of Windows")
	}
	v1.MappedPipes = mounts.mpsv1
	v2Container.MappedPipes = mounts.mpsv2

	// add assigned devices to the container definition
	if err := parseAssignedDevices(ctx, coi, v2Container); err != nil {
		return nil, nil, err
	}

	// Setup WER registry keys for local process dump creation if specified.
	// https://docs.microsoft.com/en-us/windows/win32/wer/collecting-user-mode-dumps
	procDumpLoc := coi.Spec.Annotations[oci.AnnotationProcessDumpLocation]
	if (coi.HostingSystem != nil && coi.HostingSystem.ProcessDumpLocation() != "") || procDumpLoc != "" {
		// If a process dump path was specified at pod creation time for a hypervisor isolated pod, then
		// use this value. If one was specified on the container creation document then override with this
		// instead. Unlike Linux, Windows containers can set the dump path on a per container basis.
		var dumpPath string
		if coi.HostingSystem != nil {
			dumpPath = coi.HostingSystem.ProcessDumpLocation()
		} else {
			dumpPath = procDumpLoc
		}

		dumpType, err := parseDumpType(coi.Spec.Annotations)
		if err != nil {
			return nil, nil, err
		}

		v2Container.RegistryChanges = &hcsschema.RegistryChanges{
			AddValues: []hcsschema.RegistryValue{
				{
					Key: &hcsschema.RegistryKey{
						Hive: "Software",
						Name: "Microsoft\\Windows\\Windows Error Reporting\\LocalDumps",
					},
					Name:        "DumpFolder",
					StringValue: dumpPath,
					Type_:       "String",
				},
				{
					Key: &hcsschema.RegistryKey{
						Hive: "Software",
						Name: "Microsoft\\Windows\\Windows Error Reporting\\LocalDumps",
					},
					Name:       "DumpType",
					DWordValue: dumpType,
					Type_:      "DWord",
				},
			},
		}
	}

	return v1, v2Container, nil
}

// parseAssignedDevices parses assigned devices for the container definition
// this is currently supported for v2 argon and xenon only
func parseAssignedDevices(ctx context.Context, coi *createOptionsInternal, v2 *hcsschema.Container) error {
	if !coi.isV2Argon() && !coi.isV2Xenon() {
		return nil
	}

	v2AssignedDevices := []hcsschema.Device{}
	for _, d := range coi.Spec.Windows.Devices {
		v2Dev := hcsschema.Device{}
		switch d.IDType {
		case uvm.VPCILocationPathIDType:
			v2Dev.LocationPath = d.ID
			v2Dev.Type = hcsschema.DeviceInstance
		case uvm.VPCIClassGUIDTypeLegacy:
			v2Dev.InterfaceClassGuid = d.ID
		case uvm.VPCIClassGUIDType:
			v2Dev.InterfaceClassGuid = d.ID
		default:
			return fmt.Errorf("specified device %s has unsupported type %s", d.ID, d.IDType)
		}
		log.G(ctx).WithField("hcsv2 device", v2Dev).Debug("adding assigned device to container doc")
		v2AssignedDevices = append(v2AssignedDevices, v2Dev)
	}
	v2.AssignedDevices = v2AssignedDevices
	return nil
}

// parseDumpType parses the passed in string representation of the local user mode process dump type to the
// corresponding value the registry expects to be set.
func parseDumpType(annotations map[string]string) (int32, error) {
	dmpTypeStr := annotations[oci.AnnotationWCOWProcessDumpType]
	switch dmpTypeStr {
	case "":
		// If no type specified, default to full dumps.
		return 2, nil
	case "mini":
		return 1, nil
	case "full":
		return 2, nil
	default:
		return -1, errors.New(`unknown dump type specified, valid values are "mini" or "full"`)
	}
}
