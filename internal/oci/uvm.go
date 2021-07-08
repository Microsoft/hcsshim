package oci

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	runhcsopts "github.com/Microsoft/hcsshim/cmd/containerd-shim-runhcs-v1/options"
	"github.com/Microsoft/hcsshim/internal/clone"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/logfields"
	"github.com/Microsoft/hcsshim/internal/uvm"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

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
	// AnnotationContainerExtraLayers contains a comma separated list of directories that contain a
	// layer vhd, named layer.vhd, that should additionally be added to the container's list of layers
	// to be created with.
	AnnotationContainerExtraLayers = "io.microsoft.container.extralayers"
	// AnnotationHostProcessInheritUser indicates whether to ignore the username passed in to run a host process
	// container as and instead inherit the user token from the executable that is launching the container process.
	AnnotationHostProcessInheritUser = "microsoft.com/hostprocess-inherit-user"
	// AnnotationHostProcessContainer indicates to launch a host process container (job container in this repository).
	AnnotationHostProcessContainer = "microsoft.com/hostprocess-container"

	annotationAllowOvercommit       = "io.microsoft.virtualmachine.computetopology.memory.allowovercommit"
	annotationEnableDeferredCommit  = "io.microsoft.virtualmachine.computetopology.memory.enabledeferredcommit"
	annotationEnableColdDiscardHint = "io.microsoft.virtualmachine.computetopology.memory.enablecolddiscardhint"
	// annotationMemorySizeInMB overrides the container memory size set via the
	// OCI spec.
	//
	// Note: This annotation is in MB. OCI is in Bytes. When using this override
	// the caller MUST use MB or sizing will be wrong.
	annotationMemorySizeInMB         = "io.microsoft.virtualmachine.computetopology.memory.sizeinmb"
	annotationMemoryLowMMIOGapInMB   = "io.microsoft.virtualmachine.computetopology.memory.lowmmiogapinmb"
	annotationMemoryHighMMIOBaseInMB = "io.microsoft.virtualmachine.computetopology.memory.highmmiobaseinmb"
	annotationMemoryHighMMIOGapInMB  = "io.microsoft.virtualmachine.computetopology.memory.highmmiogapinmb"
	// annotationProcessorCount overrides the hypervisor isolated vCPU count set
	// via the OCI spec.
	//
	// Note: Unlike Windows process isolated container QoS Count/Limt/Weight on
	// the UVM are not mutually exclusive and can be set together.
	annotationProcessorCount = "io.microsoft.virtualmachine.computetopology.processor.count"
	// annotationProcessorLimit overrides the hypervisor isolated vCPU limit set
	// via the OCI spec.
	//
	// Limit allows values 1 - 100,000 where 100,000 means 100% CPU. (And is the
	// default if omitted)
	//
	// Note: Unlike Windows process isolated container QoS Count/Limt/Weight on
	// the UVM are not mutually exclusive and can be set together.
	annotationProcessorLimit = "io.microsoft.virtualmachine.computetopology.processor.limit"
	// annotationProcessorWeight overrides the hypervisor isolated vCPU weight set
	// via the OCI spec.
	//
	// Weight allows values 0 - 10,000. (100 is the default if omitted)
	//
	// Note: Unlike Windows process isolated container QoS Count/Limt/Weight on
	// the UVM are not mutually exclusive and can be set together.
	annotationProcessorWeight             = "io.microsoft.virtualmachine.computetopology.processor.weight"
	annotationVPMemCount                  = "io.microsoft.virtualmachine.devices.virtualpmem.maximumcount"
	annotationVPMemSize                   = "io.microsoft.virtualmachine.devices.virtualpmem.maximumsizebytes"
	annotationPreferredRootFSType         = "io.microsoft.virtualmachine.lcow.preferredrootfstype"
	annotationBootFilesRootPath           = "io.microsoft.virtualmachine.lcow.bootfilesrootpath"
	annotationKernelDirectBoot            = "io.microsoft.virtualmachine.lcow.kerneldirectboot"
	annotationVPCIEnabled                 = "io.microsoft.virtualmachine.lcow.vpcienabled"
	annotationVPMemNoMultiMapping         = "io.microsoft.virtualmachine.lcow.vpmem.nomultimapping"
	annotationStorageQoSBandwidthMaximum  = "io.microsoft.virtualmachine.storageqos.bandwidthmaximum"
	annotationStorageQoSIopsMaximum       = "io.microsoft.virtualmachine.storageqos.iopsmaximum"
	annotationFullyPhysicallyBacked       = "io.microsoft.virtualmachine.fullyphysicallybacked"
	annotationDisableCompartmentNamespace = "io.microsoft.virtualmachine.disablecompartmentnamespace"
	annotationVSMBNoDirectMap             = "io.microsoft.virtualmachine.wcow.virtualSMB.nodirectmap"

	// annotation used to specify the cpugroup ID that a UVM should be assigned to
	annotationCPUGroupID = "io.microsoft.virtualmachine.cpugroup.id"

	// SaveAsTemplate annotation must be used with a pod & container creation request.
	// If this annotation is present in the request then it will save the UVM (pod)
	// and the container(s) inside it as a template. However, this also means that this
	// pod and the containers inside this pod will permananetly stay in the
	// paused/templated state and can not be resumed again.
	annotationSaveAsTemplate = "io.microsoft.virtualmachine.saveastemplate"

	// This annotation should be used when creating a pod or a container from a template.
	// When creating a pod from a template use the ID of the templated pod as the
	// TemplateID and when creating a container use the ID of the templated container as
	// the TemplateID. It is the client's responsibility to make sure that the sandbox
	// within which a cloned container needs to be created must also be created from the
	// same template.
	annotationTemplateID         = "io.microsoft.virtualmachine.templateid"
	annotationNetworkConfigProxy = "io.microsoft.network.ncproxy"
	AnnotationNcproxyContainerID = "io.microsoft.network.ncproxy.containerid"
)

// parseAnnotationsBool searches `a` for `key` and if found verifies that the
// value is `true` or `false` in any case. If `key` is not found returns `def`.
func parseAnnotationsBool(ctx context.Context, a map[string]string, key string, def bool) bool {
	if v, ok := a[key]; ok {
		switch strings.ToLower(v) {
		case "true":
			return true
		case "false":
			return false
		default:
			log.G(ctx).WithFields(logrus.Fields{
				logfields.OCIAnnotation: key,
				logfields.Value:         v,
				logfields.ExpectedType:  logfields.Bool,
			}).Warning("annotation could not be parsed")
		}
	}
	return def
}

// ParseAnnotationsCPUCount searches `s.Annotations` for the CPU annotation. If
// not found searches `s` for the Windows CPU section. If neither are found
// returns `def`.
func ParseAnnotationsCPUCount(ctx context.Context, s *specs.Spec, annotation string, def int32) int32 {
	if m := parseAnnotationsUint64(ctx, s.Annotations, annotation, 0); m != 0 {
		return int32(m)
	}
	if s.Windows != nil &&
		s.Windows.Resources != nil &&
		s.Windows.Resources.CPU != nil &&
		s.Windows.Resources.CPU.Count != nil &&
		*s.Windows.Resources.CPU.Count > 0 {
		return int32(*s.Windows.Resources.CPU.Count)
	}
	return def
}

// ParseAnnotationsCPULimit searches `s.Annotations` for the CPU annotation. If
// not found searches `s` for the Windows CPU section. If neither are found
// returns `def`.
func ParseAnnotationsCPULimit(ctx context.Context, s *specs.Spec, annotation string, def int32) int32 {
	if m := parseAnnotationsUint64(ctx, s.Annotations, annotation, 0); m != 0 {
		return int32(m)
	}
	if s.Windows != nil &&
		s.Windows.Resources != nil &&
		s.Windows.Resources.CPU != nil &&
		s.Windows.Resources.CPU.Maximum != nil &&
		*s.Windows.Resources.CPU.Maximum > 0 {
		return int32(*s.Windows.Resources.CPU.Maximum)
	}
	return def
}

// ParseAnnotationsCPUWeight searches `s.Annotations` for the CPU annotation. If
// not found searches `s` for the Windows CPU section. If neither are found
// returns `def`.
func ParseAnnotationsCPUWeight(ctx context.Context, s *specs.Spec, annotation string, def int32) int32 {
	if m := parseAnnotationsUint64(ctx, s.Annotations, annotation, 0); m != 0 {
		return int32(m)
	}
	if s.Windows != nil &&
		s.Windows.Resources != nil &&
		s.Windows.Resources.CPU != nil &&
		s.Windows.Resources.CPU.Shares != nil &&
		*s.Windows.Resources.CPU.Shares > 0 {
		return int32(*s.Windows.Resources.CPU.Shares)
	}
	return def
}

// ParseAnnotationsStorageIops searches `s.Annotations` for the `Iops`
// annotation. If not found searches `s` for the Windows Storage section. If
// neither are found returns `def`.
func ParseAnnotationsStorageIops(ctx context.Context, s *specs.Spec, annotation string, def int32) int32 {
	if m := parseAnnotationsUint64(ctx, s.Annotations, annotation, 0); m != 0 {
		return int32(m)
	}
	if s.Windows != nil &&
		s.Windows.Resources != nil &&
		s.Windows.Resources.Storage != nil &&
		s.Windows.Resources.Storage.Iops != nil &&
		*s.Windows.Resources.Storage.Iops > 0 {
		return int32(*s.Windows.Resources.Storage.Iops)
	}
	return def
}

// ParseAnnotationsStorageBps searches `s.Annotations` for the `Bps` annotation.
// If not found searches `s` for the Windows Storage section. If neither are
// found returns `def`.
func ParseAnnotationsStorageBps(ctx context.Context, s *specs.Spec, annotation string, def int32) int32 {
	if m := parseAnnotationsUint64(ctx, s.Annotations, annotation, 0); m != 0 {
		return int32(m)
	}
	if s.Windows != nil &&
		s.Windows.Resources != nil &&
		s.Windows.Resources.Storage != nil &&
		s.Windows.Resources.Storage.Bps != nil &&
		*s.Windows.Resources.Storage.Bps > 0 {
		return int32(*s.Windows.Resources.Storage.Bps)
	}
	return def
}

// ParseAnnotationsMemory searches `s.Annotations` for the memory annotation. If
// not found searches `s` for the Windows memory section. If neither are found
// returns `def`.
//
// Note: The returned value is in `MB`.
func ParseAnnotationsMemory(ctx context.Context, s *specs.Spec, annotation string, def uint64) uint64 {
	if m := parseAnnotationsUint64(ctx, s.Annotations, annotation, 0); m != 0 {
		return m
	}
	if s.Windows != nil &&
		s.Windows.Resources != nil &&
		s.Windows.Resources.Memory != nil &&
		s.Windows.Resources.Memory.Limit != nil &&
		*s.Windows.Resources.Memory.Limit > 0 {
		return (*s.Windows.Resources.Memory.Limit / 1024 / 1024)
	}
	return def
}

// parseAnnotationsPreferredRootFSType searches `a` for `key` and verifies that the
// value is in the set of allowed values. If `key` is not found returns `def`.
func parseAnnotationsPreferredRootFSType(ctx context.Context, a map[string]string, key string, def uvm.PreferredRootFSType) uvm.PreferredRootFSType {
	if v, ok := a[key]; ok {
		switch v {
		case "initrd":
			return uvm.PreferredRootFSTypeInitRd
		case "vhd":
			return uvm.PreferredRootFSTypeVHD
		default:
			log.G(ctx).WithFields(logrus.Fields{
				"annotation": key,
				"value":      v,
			}).Warn("annotation value must be 'initrd' or 'vhd'")
		}
	}
	return def
}

// parseAnnotationsUint32 searches `a` for `key` and if found verifies that the
// value is a 32 bit unsigned integer. If `key` is not found returns `def`.
func parseAnnotationsUint32(ctx context.Context, a map[string]string, key string, def uint32) uint32 {
	if v, ok := a[key]; ok {
		countu, err := strconv.ParseUint(v, 10, 32)
		if err == nil {
			v := uint32(countu)
			return v
		}
		log.G(ctx).WithFields(logrus.Fields{
			logfields.OCIAnnotation: key,
			logfields.Value:         v,
			logfields.ExpectedType:  logfields.Uint32,
			logrus.ErrorKey:         err,
		}).Warning("annotation could not be parsed")
	}
	return def
}

// parseAnnotationsUint64 searches `a` for `key` and if found verifies that the
// value is a 64 bit unsigned integer. If `key` is not found returns `def`.
func parseAnnotationsUint64(ctx context.Context, a map[string]string, key string, def uint64) uint64 {
	if v, ok := a[key]; ok {
		countu, err := strconv.ParseUint(v, 10, 64)
		if err == nil {
			return countu
		}
		log.G(ctx).WithFields(logrus.Fields{
			logfields.OCIAnnotation: key,
			logfields.Value:         v,
			logfields.ExpectedType:  logfields.Uint64,
			logrus.ErrorKey:         err,
		}).Warning("annotation could not be parsed")
	}
	return def
}

// parseAnnotationsString searches `a` for `key`. If `key` is not found returns `def`.
func parseAnnotationsString(a map[string]string, key string, def string) string {
	if v, ok := a[key]; ok {
		return v
	}
	return def
}

// ParseAnnotationsSaveAsTemplate searches for the boolean value which specifies
// if this create request should be considered as a template creation request. If value
// is found the returns the actual value, returns false otherwise.
func ParseAnnotationsSaveAsTemplate(ctx context.Context, s *specs.Spec) bool {
	return parseAnnotationsBool(ctx, s.Annotations, annotationSaveAsTemplate, false)
}

// ParseAnnotationsTemplateID searches for the templateID in the create request. If the
// value is found then returns the value otherwise returns the empty string.
func ParseAnnotationsTemplateID(ctx context.Context, s *specs.Spec) string {
	return parseAnnotationsString(s.Annotations, annotationTemplateID, "")
}

func ParseCloneAnnotations(ctx context.Context, s *specs.Spec) (isTemplate bool, templateID string, err error) {
	templateID = ParseAnnotationsTemplateID(ctx, s)
	isTemplate = ParseAnnotationsSaveAsTemplate(ctx, s)
	if templateID != "" && isTemplate {
		return false, "", fmt.Errorf("templateID and save as template flags can not be passed in the same request")
	}

	if (isTemplate || templateID != "") && !IsWCOW(s) {
		return false, "", fmt.Errorf("save as template and creating clones is only available for WCOW")
	}
	return
}

// handleAnnotationKernelDirectBoot handles parsing annotationKernelDirectBoot and setting
// implied annotations from the result.
func handleAnnotationKernelDirectBoot(ctx context.Context, a map[string]string, lopts *uvm.OptionsLCOW) {
	lopts.KernelDirect = parseAnnotationsBool(ctx, a, annotationKernelDirectBoot, lopts.KernelDirect)
	if !lopts.KernelDirect {
		lopts.KernelFile = uvm.KernelFile
	}
}

// handleAnnotationPreferredRootFSType handles parsing annotationPreferredRootFSType and setting
// implied annotations from the result
func handleAnnotationPreferredRootFSType(ctx context.Context, a map[string]string, lopts *uvm.OptionsLCOW) {
	lopts.PreferredRootFSType = parseAnnotationsPreferredRootFSType(ctx, a, annotationPreferredRootFSType, lopts.PreferredRootFSType)
	switch lopts.PreferredRootFSType {
	case uvm.PreferredRootFSTypeInitRd:
		lopts.RootFSFile = uvm.InitrdFile
	case uvm.PreferredRootFSTypeVHD:
		lopts.RootFSFile = uvm.VhdFile
	}
}

// handleAnnotationFullyPhysicallyBacked handles parsing annotationFullyPhysicallyBacked and setting
// implied annotations from the result. For both LCOW and WCOW options.
func handleAnnotationFullyPhysicallyBacked(ctx context.Context, a map[string]string, opts interface{}) {
	switch options := opts.(type) {
	case *uvm.OptionsLCOW:
		options.FullyPhysicallyBacked = parseAnnotationsBool(ctx, a, annotationFullyPhysicallyBacked, options.FullyPhysicallyBacked)
		if options.FullyPhysicallyBacked {
			options.AllowOvercommit = false
			options.PreferredRootFSType = uvm.PreferredRootFSTypeInitRd
			options.RootFSFile = uvm.InitrdFile
			options.VPMemDeviceCount = 0
		}
	case *uvm.OptionsWCOW:
		options.FullyPhysicallyBacked = parseAnnotationsBool(ctx, a, annotationFullyPhysicallyBacked, options.FullyPhysicallyBacked)
		if options.FullyPhysicallyBacked {
			options.AllowOvercommit = false
		}
	}
}

// handleCloneAnnotations handles parsing annotations related to template creation and cloning
// Since late cloning is only supported for WCOW this function only deals with WCOW options.
func handleCloneAnnotations(ctx context.Context, a map[string]string, wopts *uvm.OptionsWCOW) (err error) {
	wopts.IsTemplate = parseAnnotationsBool(ctx, a, annotationSaveAsTemplate, false)
	templateID := parseAnnotationsString(a, annotationTemplateID, "")
	if templateID != "" {
		tc, err := clone.FetchTemplateConfig(ctx, templateID)
		if err != nil {
			return err
		}
		wopts.TemplateConfig = &uvm.UVMTemplateConfig{
			UVMID:      tc.TemplateUVMID,
			CreateOpts: tc.TemplateUVMCreateOpts,
			Resources:  tc.TemplateUVMResources,
		}
		wopts.IsClone = true
	}
	return nil
}

// SpecToUVMCreateOpts parses `s` and returns either `*uvm.OptionsLCOW` or
// `*uvm.OptionsWCOW`.
func SpecToUVMCreateOpts(ctx context.Context, s *specs.Spec, id, owner string) (interface{}, error) {
	if !IsIsolated(s) {
		return nil, errors.New("cannot create UVM opts for non-isolated spec")
	}
	if IsLCOW(s) {
		lopts := uvm.NewDefaultOptionsLCOW(id, owner)
		lopts.MemorySizeInMB = ParseAnnotationsMemory(ctx, s, annotationMemorySizeInMB, lopts.MemorySizeInMB)
		lopts.LowMMIOGapInMB = parseAnnotationsUint64(ctx, s.Annotations, annotationMemoryLowMMIOGapInMB, lopts.LowMMIOGapInMB)
		lopts.HighMMIOBaseInMB = parseAnnotationsUint64(ctx, s.Annotations, annotationMemoryHighMMIOBaseInMB, lopts.HighMMIOBaseInMB)
		lopts.HighMMIOGapInMB = parseAnnotationsUint64(ctx, s.Annotations, annotationMemoryHighMMIOGapInMB, lopts.HighMMIOGapInMB)
		lopts.AllowOvercommit = parseAnnotationsBool(ctx, s.Annotations, annotationAllowOvercommit, lopts.AllowOvercommit)
		lopts.EnableDeferredCommit = parseAnnotationsBool(ctx, s.Annotations, annotationEnableDeferredCommit, lopts.EnableDeferredCommit)
		lopts.EnableColdDiscardHint = parseAnnotationsBool(ctx, s.Annotations, annotationEnableColdDiscardHint, lopts.EnableColdDiscardHint)
		lopts.ProcessorCount = ParseAnnotationsCPUCount(ctx, s, annotationProcessorCount, lopts.ProcessorCount)
		lopts.ProcessorLimit = ParseAnnotationsCPULimit(ctx, s, annotationProcessorLimit, lopts.ProcessorLimit)
		lopts.ProcessorWeight = ParseAnnotationsCPUWeight(ctx, s, annotationProcessorWeight, lopts.ProcessorWeight)
		lopts.VPMemDeviceCount = parseAnnotationsUint32(ctx, s.Annotations, annotationVPMemCount, lopts.VPMemDeviceCount)
		lopts.VPMemSizeBytes = parseAnnotationsUint64(ctx, s.Annotations, annotationVPMemSize, lopts.VPMemSizeBytes)
		lopts.VPMemNoMultiMapping = parseAnnotationsBool(ctx, s.Annotations, annotationVPMemNoMultiMapping, lopts.VPMemNoMultiMapping)
		lopts.StorageQoSBandwidthMaximum = ParseAnnotationsStorageBps(ctx, s, annotationStorageQoSBandwidthMaximum, lopts.StorageQoSBandwidthMaximum)
		lopts.StorageQoSIopsMaximum = ParseAnnotationsStorageIops(ctx, s, annotationStorageQoSIopsMaximum, lopts.StorageQoSIopsMaximum)
		lopts.VPCIEnabled = parseAnnotationsBool(ctx, s.Annotations, annotationVPCIEnabled, lopts.VPCIEnabled)
		lopts.BootFilesPath = parseAnnotationsString(s.Annotations, annotationBootFilesRootPath, lopts.BootFilesPath)
		lopts.CPUGroupID = parseAnnotationsString(s.Annotations, annotationCPUGroupID, lopts.CPUGroupID)
		lopts.NetworkConfigProxy = parseAnnotationsString(s.Annotations, annotationNetworkConfigProxy, lopts.NetworkConfigProxy)
		handleAnnotationPreferredRootFSType(ctx, s.Annotations, lopts)
		handleAnnotationKernelDirectBoot(ctx, s.Annotations, lopts)

		// parsing of FullyPhysicallyBacked needs to go after handling kernel direct boot and
		// preferred rootfs type since it may overwrite settings created by those
		handleAnnotationFullyPhysicallyBacked(ctx, s.Annotations, lopts)
		return lopts, nil
	} else if IsWCOW(s) {
		wopts := uvm.NewDefaultOptionsWCOW(id, owner)
		wopts.MemorySizeInMB = ParseAnnotationsMemory(ctx, s, annotationMemorySizeInMB, wopts.MemorySizeInMB)
		wopts.LowMMIOGapInMB = parseAnnotationsUint64(ctx, s.Annotations, annotationMemoryLowMMIOGapInMB, wopts.LowMMIOGapInMB)
		wopts.HighMMIOBaseInMB = parseAnnotationsUint64(ctx, s.Annotations, annotationMemoryHighMMIOBaseInMB, wopts.HighMMIOBaseInMB)
		wopts.HighMMIOGapInMB = parseAnnotationsUint64(ctx, s.Annotations, annotationMemoryHighMMIOGapInMB, wopts.HighMMIOGapInMB)
		wopts.AllowOvercommit = parseAnnotationsBool(ctx, s.Annotations, annotationAllowOvercommit, wopts.AllowOvercommit)
		wopts.EnableDeferredCommit = parseAnnotationsBool(ctx, s.Annotations, annotationEnableDeferredCommit, wopts.EnableDeferredCommit)
		wopts.ProcessorCount = ParseAnnotationsCPUCount(ctx, s, annotationProcessorCount, wopts.ProcessorCount)
		wopts.ProcessorLimit = ParseAnnotationsCPULimit(ctx, s, annotationProcessorLimit, wopts.ProcessorLimit)
		wopts.ProcessorWeight = ParseAnnotationsCPUWeight(ctx, s, annotationProcessorWeight, wopts.ProcessorWeight)
		wopts.StorageQoSBandwidthMaximum = ParseAnnotationsStorageBps(ctx, s, annotationStorageQoSBandwidthMaximum, wopts.StorageQoSBandwidthMaximum)
		wopts.StorageQoSIopsMaximum = ParseAnnotationsStorageIops(ctx, s, annotationStorageQoSIopsMaximum, wopts.StorageQoSIopsMaximum)
		wopts.DisableCompartmentNamespace = parseAnnotationsBool(ctx, s.Annotations, annotationDisableCompartmentNamespace, wopts.DisableCompartmentNamespace)
		wopts.CPUGroupID = parseAnnotationsString(s.Annotations, annotationCPUGroupID, wopts.CPUGroupID)
		wopts.NetworkConfigProxy = parseAnnotationsString(s.Annotations, annotationNetworkConfigProxy, wopts.NetworkConfigProxy)
		wopts.NoDirectMap = parseAnnotationsBool(ctx, s.Annotations, annotationVSMBNoDirectMap, wopts.NoDirectMap)
		handleAnnotationFullyPhysicallyBacked(ctx, s.Annotations, wopts)
		if err := handleCloneAnnotations(ctx, s.Annotations, wopts); err != nil {
			return nil, err
		}
		return wopts, nil
	}
	return nil, errors.New("cannot create UVM opts spec is not LCOW or WCOW")
}

// UpdateSpecFromOptions sets extra annotations on the OCI spec based on the
// `opts` struct.
func UpdateSpecFromOptions(s specs.Spec, opts *runhcsopts.Options) specs.Spec {
	if opts == nil {
		return s
	}

	if _, ok := s.Annotations[annotationBootFilesRootPath]; !ok && opts.BootFilesRootPath != "" {
		s.Annotations[annotationBootFilesRootPath] = opts.BootFilesRootPath
	}

	if _, ok := s.Annotations[annotationProcessorCount]; !ok && opts.VmProcessorCount != 0 {
		s.Annotations[annotationProcessorCount] = strconv.FormatInt(int64(opts.VmProcessorCount), 10)
	}

	if _, ok := s.Annotations[annotationMemorySizeInMB]; !ok && opts.VmMemorySizeInMb != 0 {
		s.Annotations[annotationMemorySizeInMB] = strconv.FormatInt(int64(opts.VmMemorySizeInMb), 10)
	}

	if _, ok := s.Annotations[AnnotationGPUVHDPath]; !ok && opts.GPUVHDPath != "" {
		s.Annotations[AnnotationGPUVHDPath] = opts.GPUVHDPath
	}

	if _, ok := s.Annotations[annotationNetworkConfigProxy]; !ok && opts.NCProxyAddr != "" {
		s.Annotations[annotationNetworkConfigProxy] = opts.NCProxyAddr
	}

	return s
}
