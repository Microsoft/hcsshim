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
	return parseAnnotationsBool(ctx, s.Annotations, AnnotationSaveAsTemplate, false)
}

// ParseAnnotationsTemplateID searches for the templateID in the create request. If the
// value is found then returns the value otherwise returns the empty string.
func ParseAnnotationsTemplateID(ctx context.Context, s *specs.Spec) string {
	return parseAnnotationsString(s.Annotations, AnnotationTemplateID, "")
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
	lopts.KernelDirect = parseAnnotationsBool(ctx, a, AnnotationKernelDirectBoot, lopts.KernelDirect)
	if !lopts.KernelDirect {
		lopts.KernelFile = uvm.KernelFile
	}
}

// handleAnnotationPreferredRootFSType handles parsing annotationPreferredRootFSType and setting
// implied annotations from the result
func handleAnnotationPreferredRootFSType(ctx context.Context, a map[string]string, lopts *uvm.OptionsLCOW) {
	lopts.PreferredRootFSType = parseAnnotationsPreferredRootFSType(ctx, a, AnnotationPreferredRootFSType, lopts.PreferredRootFSType)
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
		options.FullyPhysicallyBacked = parseAnnotationsBool(ctx, a, AnnotationFullyPhysicallyBacked, options.FullyPhysicallyBacked)
		if options.FullyPhysicallyBacked {
			options.AllowOvercommit = false
			options.PreferredRootFSType = uvm.PreferredRootFSTypeInitRd
			options.RootFSFile = uvm.InitrdFile
			options.VPMemDeviceCount = 0
		}
	case *uvm.OptionsWCOW:
		options.FullyPhysicallyBacked = parseAnnotationsBool(ctx, a, AnnotationFullyPhysicallyBacked, options.FullyPhysicallyBacked)
		if options.FullyPhysicallyBacked {
			options.AllowOvercommit = false
		}
	}
}

// handleCloneAnnotations handles parsing annotations related to template creation and cloning
// Since late cloning is only supported for WCOW this function only deals with WCOW options.
func handleCloneAnnotations(ctx context.Context, a map[string]string, wopts *uvm.OptionsWCOW) (err error) {
	wopts.IsTemplate = parseAnnotationsBool(ctx, a, AnnotationSaveAsTemplate, false)
	templateID := parseAnnotationsString(a, AnnotationTemplateID, "")
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
		lopts.MemorySizeInMB = ParseAnnotationsMemory(ctx, s, AnnotationMemorySizeInMB, lopts.MemorySizeInMB)
		lopts.LowMMIOGapInMB = parseAnnotationsUint64(ctx, s.Annotations, AnnotationMemoryLowMMIOGapInMB, lopts.LowMMIOGapInMB)
		lopts.HighMMIOBaseInMB = parseAnnotationsUint64(ctx, s.Annotations, AnnotationMemoryHighMMIOBaseInMB, lopts.HighMMIOBaseInMB)
		lopts.HighMMIOGapInMB = parseAnnotationsUint64(ctx, s.Annotations, AnnotationMemoryHighMMIOGapInMB, lopts.HighMMIOGapInMB)
		lopts.AllowOvercommit = parseAnnotationsBool(ctx, s.Annotations, AnnotationAllowOvercommit, lopts.AllowOvercommit)
		lopts.EnableDeferredCommit = parseAnnotationsBool(ctx, s.Annotations, AnnotationEnableDeferredCommit, lopts.EnableDeferredCommit)
		lopts.EnableColdDiscardHint = parseAnnotationsBool(ctx, s.Annotations, AnnotationEnableColdDiscardHint, lopts.EnableColdDiscardHint)
		lopts.ProcessorCount = ParseAnnotationsCPUCount(ctx, s, AnnotationProcessorCount, lopts.ProcessorCount)
		lopts.ProcessorLimit = ParseAnnotationsCPULimit(ctx, s, AnnotationProcessorLimit, lopts.ProcessorLimit)
		lopts.ProcessorWeight = ParseAnnotationsCPUWeight(ctx, s, AnnotationProcessorWeight, lopts.ProcessorWeight)
		lopts.VPMemDeviceCount = parseAnnotationsUint32(ctx, s.Annotations, AnnotationVPMemCount, lopts.VPMemDeviceCount)
		lopts.VPMemSizeBytes = parseAnnotationsUint64(ctx, s.Annotations, AnnotationVPMemSize, lopts.VPMemSizeBytes)
		lopts.VPMemNoMultiMapping = parseAnnotationsBool(ctx, s.Annotations, AnnotationVPMemNoMultiMapping, lopts.VPMemNoMultiMapping)
		lopts.StorageQoSBandwidthMaximum = ParseAnnotationsStorageBps(ctx, s, AnnotationStorageQoSBandwidthMaximum, lopts.StorageQoSBandwidthMaximum)
		lopts.StorageQoSIopsMaximum = ParseAnnotationsStorageIops(ctx, s, AnnotationStorageQoSIopsMaximum, lopts.StorageQoSIopsMaximum)
		lopts.VPCIEnabled = parseAnnotationsBool(ctx, s.Annotations, AnnotationVPCIEnabled, lopts.VPCIEnabled)
		lopts.BootFilesPath = parseAnnotationsString(s.Annotations, AnnotationBootFilesRootPath, lopts.BootFilesPath)
		lopts.CPUGroupID = parseAnnotationsString(s.Annotations, AnnotationCPUGroupID, lopts.CPUGroupID)
		lopts.NetworkConfigProxy = parseAnnotationsString(s.Annotations, AnnotationNetworkConfigProxy, lopts.NetworkConfigProxy)
		lopts.ProcessDumpLocation = parseAnnotationsString(s.Annotations, AnnotationProcessDumpLocation, lopts.ProcessDumpLocation)
		handleAnnotationPreferredRootFSType(ctx, s.Annotations, lopts)
		handleAnnotationKernelDirectBoot(ctx, s.Annotations, lopts)

		// parsing of FullyPhysicallyBacked needs to go after handling kernel direct boot and
		// preferred rootfs type since it may overwrite settings created by those
		handleAnnotationFullyPhysicallyBacked(ctx, s.Annotations, lopts)
		return lopts, nil
	} else if IsWCOW(s) {
		wopts := uvm.NewDefaultOptionsWCOW(id, owner)
		wopts.MemorySizeInMB = ParseAnnotationsMemory(ctx, s, AnnotationMemorySizeInMB, wopts.MemorySizeInMB)
		wopts.LowMMIOGapInMB = parseAnnotationsUint64(ctx, s.Annotations, AnnotationMemoryLowMMIOGapInMB, wopts.LowMMIOGapInMB)
		wopts.HighMMIOBaseInMB = parseAnnotationsUint64(ctx, s.Annotations, AnnotationMemoryHighMMIOBaseInMB, wopts.HighMMIOBaseInMB)
		wopts.HighMMIOGapInMB = parseAnnotationsUint64(ctx, s.Annotations, AnnotationMemoryHighMMIOGapInMB, wopts.HighMMIOGapInMB)
		wopts.AllowOvercommit = parseAnnotationsBool(ctx, s.Annotations, AnnotationAllowOvercommit, wopts.AllowOvercommit)
		wopts.EnableDeferredCommit = parseAnnotationsBool(ctx, s.Annotations, AnnotationEnableDeferredCommit, wopts.EnableDeferredCommit)
		wopts.ProcessorCount = ParseAnnotationsCPUCount(ctx, s, AnnotationProcessorCount, wopts.ProcessorCount)
		wopts.ProcessorLimit = ParseAnnotationsCPULimit(ctx, s, AnnotationProcessorLimit, wopts.ProcessorLimit)
		wopts.ProcessorWeight = ParseAnnotationsCPUWeight(ctx, s, AnnotationProcessorWeight, wopts.ProcessorWeight)
		wopts.StorageQoSBandwidthMaximum = ParseAnnotationsStorageBps(ctx, s, AnnotationStorageQoSBandwidthMaximum, wopts.StorageQoSBandwidthMaximum)
		wopts.StorageQoSIopsMaximum = ParseAnnotationsStorageIops(ctx, s, AnnotationStorageQoSIopsMaximum, wopts.StorageQoSIopsMaximum)
		wopts.DisableCompartmentNamespace = parseAnnotationsBool(ctx, s.Annotations, AnnotationDisableCompartmentNamespace, wopts.DisableCompartmentNamespace)
		wopts.CPUGroupID = parseAnnotationsString(s.Annotations, AnnotationCPUGroupID, wopts.CPUGroupID)
		wopts.NetworkConfigProxy = parseAnnotationsString(s.Annotations, AnnotationNetworkConfigProxy, wopts.NetworkConfigProxy)
		wopts.NoDirectMap = parseAnnotationsBool(ctx, s.Annotations, AnnotationVSMBNoDirectMap, wopts.NoDirectMap)
		wopts.ProcessDumpLocation = parseAnnotationsString(s.Annotations, AnnotationProcessDumpLocation, wopts.ProcessDumpLocation)
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

	if _, ok := s.Annotations[AnnotationBootFilesRootPath]; !ok && opts.BootFilesRootPath != "" {
		s.Annotations[AnnotationBootFilesRootPath] = opts.BootFilesRootPath
	}

	if _, ok := s.Annotations[AnnotationProcessorCount]; !ok && opts.VmProcessorCount != 0 {
		s.Annotations[AnnotationProcessorCount] = strconv.FormatInt(int64(opts.VmProcessorCount), 10)
	}

	if _, ok := s.Annotations[AnnotationMemorySizeInMB]; !ok && opts.VmMemorySizeInMb != 0 {
		s.Annotations[AnnotationMemorySizeInMB] = strconv.FormatInt(int64(opts.VmMemorySizeInMb), 10)
	}

	if _, ok := s.Annotations[AnnotationGPUVHDPath]; !ok && opts.GPUVHDPath != "" {
		s.Annotations[AnnotationGPUVHDPath] = opts.GPUVHDPath
	}

	if _, ok := s.Annotations[AnnotationNetworkConfigProxy]; !ok && opts.NCProxyAddr != "" {
		s.Annotations[AnnotationNetworkConfigProxy] = opts.NCProxyAddr
	}

	return s
}
