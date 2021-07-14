package oci

const (
	// AnnotationContainerMemorySizeInMB overrides the container memory size set
	// via the OCI spec.
	//
	// Note: This annotation is in MB. OCI is in Bytes. When using this override
	// the caller MUST use MB or sizing will be wrong.
	//
	// Note: This is only present because CRI does not (currently) have a
	// `WindowsPodSandboxConfig` for setting this correctly. It should not be
	// used via OCI runtimes and rather use
	// `spec.Windows.Resources.Memory.Limit`.
	AnnotationContainerMemorySizeInMB = "io.microsoft.container.memory.sizeinmb"

	// AnnotationContainerProcessorCount overrides the container processor count
	// set via the OCI spec.
	//
	// Note: For Windows Process Containers CPU Count/Limit/Weight are mutually
	// exclusive and the caller MUST only set one of the values.
	//
	// Note: This is only present because CRI does not (currently) have a
	// `WindowsPodSandboxConfig` for setting this correctly. It should not be
	// used via OCI runtimes and rather use `spec.Windows.Resources.CPU.Count`.
	AnnotationContainerProcessorCount = "io.microsoft.container.processor.count"

	// AnnotationContainerProcessorLimit overrides the container processor limit
	// set via the OCI spec.
	//
	// Limit allows values 1 - 10,000 where 10,000 means 100% CPU. (And is the
	// default if omitted)
	//
	// Note: For Windows Process Containers CPU Count/Limit/Weight are mutually
	// exclusive and the caller MUST only set one of the values.
	//
	// Note: This is only present because CRI does not (currently) have a
	// `WindowsPodSandboxConfig` for setting this correctly. It should not be
	// used via OCI runtimes and rather use
	// `spec.Windows.Resources.CPU.Maximum`.
	AnnotationContainerProcessorLimit = "io.microsoft.container.processor.limit"

	// AnnotationContainerProcessorWeight overrides the container processor
	// weight set via the OCI spec.
	//
	// Weight allows values 0 - 10,000. (100 is the default)
	//
	// Note: For Windows Process Containers CPU Count/Limit/Weight are mutually
	// exclusive and the caller MUST only set one of the values.
	//
	// Note: This is only present because CRI does not (currently) have a
	// `WindowsPodSandboxConfig` for setting this correctly. It should not be
	// used via OCI runtimes and rather use `spec.Windows.Resources.CPU.Shares`.
	AnnotationContainerProcessorWeight = "io.microsoft.container.processor.weight"

	// AnnotationContainerStorageQoSBandwidthMaximum overrides the container
	// storage bandwidth per second set via the OCI spec.
	//
	// Note: This is only present because CRI does not (currently) have a
	// `WindowsPodSandboxConfig` for setting this correctly. It should not be
	// used via OCI runtimes and rather use
	// `spec.Windows.Resources.Storage.Bps`.
	AnnotationContainerStorageQoSBandwidthMaximum = "io.microsoft.container.storage.qos.bandwidthmaximum"

	// AnnotationContainerStorageQoSIopsMaximum overrides the container storage
	// maximum iops set via the OCI spec.
	//
	// Note: This is only present because CRI does not (currently) have a
	// `WindowsPodSandboxConfig` for setting this correctly. It should not be
	// used via OCI runtimes and rather use
	// `spec.Windows.Resources.Storage.Iops`.
	AnnotationContainerStorageQoSIopsMaximum = "io.microsoft.container.storage.qos.iopsmaximum"

	// AnnotationGPUVHDPath overrides the default path to search for the gpu vhd
	AnnotationGPUVHDPath = "io.microsoft.lcow.gpuvhdpath"

	// AnnotationAssignedDeviceKernelDrivers indicates what drivers to install in the pod during device
	// assignment. This value should contain a list of comma separated directories containing all
	// files and information needed to install given driver(s). This may include .sys,
	// .inf, .cer, and/or other files used during standard installation with pnputil.
	AnnotationAssignedDeviceKernelDrivers = "io.microsoft.assigneddevice.kerneldrivers"

	// AnnotationHostProcessInheritUser indicates whether to ignore the username passed in to run a host process
	// container as and instead inherit the user token from the executable that is launching the container process.
	AnnotationHostProcessInheritUser = "microsoft.com/hostprocess-inherit-user"

	// AnnotationHostProcessContainer indicates to launch a host process container (job container in this repository).
	AnnotationHostProcessContainer = "microsoft.com/hostprocess-container"

	// AnnotationAllowOvercommit indicates if we should allow over commit memory for UVM.
	// Defaults to true. For physical backed memory, set to false.
	AnnotationAllowOvercommit = "io.microsoft.virtualmachine.computetopology.memory.allowovercommit"

	// AnnotationEnableDeferredCommit indicates if we should allow deferred memory commit for UVM.
	// Defaults to false. For virtual memory with deferred commit, set to true.
	AnnotationEnableDeferredCommit = "io.microsoft.virtualmachine.computetopology.memory.enabledeferredcommit"

	// AnnotationEnableColdDiscardHint indicates whether to enable cold discard hint, which allows the UVM
	// to trim non-zeroed pages from the working set (if supported by the guest operating system).
	AnnotationEnableColdDiscardHint = "io.microsoft.virtualmachine.computetopology.memory.enablecolddiscardhint"

	// AnnotationMemorySizeInMB overrides the container memory size set via the
	// OCI spec.
	//
	// Note: This annotation is in MB. OCI is in Bytes. When using this override
	// the caller MUST use MB or sizing will be wrong.
	AnnotationMemorySizeInMB = "io.microsoft.virtualmachine.computetopology.memory.sizeinmb"

	// AnnotationMemoryLowMMIOGapInMB indicates the low MMIO gap in MB
	AnnotationMemoryLowMMIOGapInMB = "io.microsoft.virtualmachine.computetopology.memory.lowmmiogapinmb"

	// AnnotationMemoryHighMMIOBaseInMB indicates the high MMIO base in MB
	AnnotationMemoryHighMMIOBaseInMB = "io.microsoft.virtualmachine.computetopology.memory.highmmiobaseinmb"

	// AnnotationMemoryHighMMIOBaseInMB indicates the high MMIO gap in MB
	AnnotationMemoryHighMMIOGapInMB = "io.microsoft.virtualmachine.computetopology.memory.highmmiogapinmb"

	// annotationProcessorCount overrides the hypervisor isolated vCPU count set
	// via the OCI spec.
	//
	// Note: Unlike Windows process isolated container QoS Count/Limt/Weight on
	// the UVM are not mutually exclusive and can be set together.
	AnnotationProcessorCount = "io.microsoft.virtualmachine.computetopology.processor.count"

	// annotationProcessorLimit overrides the hypervisor isolated vCPU limit set
	// via the OCI spec.
	//
	// Limit allows values 1 - 100,000 where 100,000 means 100% CPU. (And is the
	// default if omitted)
	//
	// Note: Unlike Windows process isolated container QoS Count/Limt/Weight on
	// the UVM are not mutually exclusive and can be set together.
	AnnotationProcessorLimit = "io.microsoft.virtualmachine.computetopology.processor.limit"

	// AnnotationProcessorWeight overrides the hypervisor isolated vCPU weight set
	// via the OCI spec.
	//
	// Weight allows values 0 - 10,000. (100 is the default if omitted)
	//
	// Note: Unlike Windows process isolated container QoS Count/Limt/Weight on
	// the UVM are not mutually exclusive and can be set together.
	AnnotationProcessorWeight = "io.microsoft.virtualmachine.computetopology.processor.weight"

	// AnnotationVPMemCount indicates the max number of vpmem devices that can be used on the UVM
	AnnotationVPMemCount = "io.microsoft.virtualmachine.devices.virtualpmem.maximumcount"

	// AnnotationVPMemSize indicates the size of the VPMem devices.
	AnnotationVPMemSize = "io.microsoft.virtualmachine.devices.virtualpmem.maximumsizebytes"

	// AnnotationPreferredRootFSType indicates what the preferred rootfs type should be for an LCOW UVM.
	// valid values are "initrd" or "vhd"
	AnnotationPreferredRootFSType = "io.microsoft.virtualmachine.lcow.preferredrootfstype"

	// AnnotationBootFilesRootPath indicates the path to find the LCOW boot files to use when creating the UVM
	AnnotationBootFilesRootPath = "io.microsoft.virtualmachine.lcow.bootfilesrootpath"

	// AnnotationKernelDirectBoot indicates that we should skip UEFI and boot directly to `kernel`
	AnnotationKernelDirectBoot = "io.microsoft.virtualmachine.lcow.kerneldirectboot"

	// AnnotationVPCIEnabled indicates that pci support should be enabled for the LCOW UVM
	AnnotationVPCIEnabled = "io.microsoft.virtualmachine.lcow.vpcienabled"

	// AnnotationVPMemNoMultiMapping indicates that we should disable LCOW vpmem layer multi mapping
	AnnotationVPMemNoMultiMapping = "io.microsoft.virtualmachine.lcow.vpmem.nomultimapping"

	// AnnotationStorageQoSBandwidthMaximum indicates the maximum number of bytes per second. If `0`
	// will default to the platform default.
	AnnotationStorageQoSBandwidthMaximum = "io.microsoft.virtualmachine.storageqos.bandwidthmaximum"

	// AnnotationStorageQoSIopsMaximum indicates the maximum number of Iops. If `0` will
	// default to the platform default.
	AnnotationStorageQoSIopsMaximum = "io.microsoft.virtualmachine.storageqos.iopsmaximum"

	// AnnotationFullyPhysicallyBacked indicates that the UVM should use physically backed memory only,
	// including for additional devices added later.
	AnnotationFullyPhysicallyBacked = "io.microsoft.virtualmachine.fullyphysicallybacked"

	// AnnotationDisableCompartmentNamespace sets whether to disable namespacing the network compartment in the UVM
	// for WCOW.
	AnnotationDisableCompartmentNamespace = "io.microsoft.virtualmachine.disablecompartmentnamespace"

	// AnnotationVSMBNoDirectMap specifies that no direct mapping should be used for any VSMBs added to the UVM
	AnnotationVSMBNoDirectMap = "io.microsoft.virtualmachine.wcow.virtualSMB.nodirectmap"

	// AnnotationCPUGroupID specifies the cpugroup ID that a UVM should be assigned to if any
	AnnotationCPUGroupID = "io.microsoft.virtualmachine.cpugroup.id"

	// AnnotationSaveAsTemplate annotation must be used with a pod & container creation request.
	// If this annotation is present in the request then it will save the UVM (pod)
	// and the container(s) inside it as a template. However, this also means that this
	// pod and the containers inside this pod will permananetly stay in the
	// paused/templated state and can not be resumed again.
	AnnotationSaveAsTemplate = "io.microsoft.virtualmachine.saveastemplate"

	// AnnotationTemplateID should be used when creating a pod or a container from a template.
	// When creating a pod from a template use the ID of the templated pod as the
	// TemplateID and when creating a container use the ID of the templated container as
	// the TemplateID. It is the client's responsibility to make sure that the sandbox
	// within which a cloned container needs to be created must also be created from the
	// same template.
	AnnotationTemplateID = "io.microsoft.virtualmachine.templateid"

	// AnnotationNetworkConfigProxy holds the address of the network config proxy service.
	// If set, network setup will be attempted via ncproxy.
	AnnotationNetworkConfigProxy = "io.microsoft.network.ncproxy"

	// AnnotationNcproxyContainerID indicates whether or not to use the hcsshim container ID
	// when setting up ncproxy and computeagent
	AnnotationNcproxyContainerID = "io.microsoft.network.ncproxy.containerid"

	// AnnotationProcessDumpLocation specifies a path inside of containers to save process dumps to. As
	// the scratch space for a container is generally cleaned up after exit, this is best set to a volume mount of
	// some kind (vhd, bind mount, fileshare mount etc.)
	AnnotationProcessDumpLocation = "io.microsoft.processdumplocation"

	// AnnotationWCOWProcessDumpType specifies the type of dump to create when generating a local user mode
	// process dump for Windows containers. The supported options are "mini", and "full".
	// See DumpType: https://docs.microsoft.com/en-us/windows/win32/wer/collecting-user-mode-dumps
	AnnotationWCOWProcessDumpType = "io.microsoft.wcow.processdumptype"

	// AnnotationRLimitCore specifies the core rlimit value for a container. This will need to be set
	// in order to have core dumps generated for a given container.
	AnnotationRLimitCore = "io.microsoft.lcow.rlimitcore"
)
