package uvm

import (
	"context"
	"fmt"

	"github.com/Microsoft/hcsshim/internal/cow"
	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/pkg/errors"
)

const (
	// default namespace ID used for all template and clone VMs.
	DEFAULT_CLONE_NETWORK_NAMESPACE_ID = "89EB8A86-E253-41FD-9800-E6D88EB2E18A"
)

// Cloneable is a generic interface for cloning a specific resource. Not all resources can
// be cloned and so all resources might not implement this interface. This interface is
// mainly used during late cloning process to clone the resources associated with the UVM
// and the container. For some resources (like scratch VHDs of the UVM & container)
// cloning means actually creating a copy of that resource while for some resources it
// simply means adding that resource to the cloned VM without copying (like VSMB shares).
// The Clone function of that resource will deal with these details.
type Cloneable interface {
	// A resource that supports cloning should also support serialization and
	// deserialization operations. This is because during resource cloning a resource
	// is usually serialized in one process and then deserialized and cloned in some
	// other process. Care should be taken while serializing a resource to not include
	// any state that will not be valid during the deserialization step. By default
	// gob encoding is used to serialize and deserialize resources but a resource can
	// implement `gob.GobEncoder` & `gob.GobDecoder` interfaces to provide its own
	// serialization and deserialization functions.

	// A SerialVersionID is an identifier used to recognize a unique version of a
	// resource. Every time the definition of the resource struct changes this ID is
	// bumped up.  This ID is used to ensure that we serialize and deserialize the
	// same version of a resource.
	GetSerialVersionID() uint32

	// Clone function creates a clone of the resource on the UVM `vm` (i.e adds the
	// cloned resource to the `vm`)
	// `cd` parameter can be used to pass any other data that is required during the
	// cloning process of that resource (for example, when cloning SCSI Mounts we
	// might need scratchFolder).
	// Clone function should be called on a valid struct (Mostly on the struct which
	// is deserialized, and so Clone function should only depend on the fields that
	// are exported in the struct).
	// The implementation of the clone function should avoid reading any data from the
	// `vm` struct, it can add new fields to the vm struct but since the vm struct
	// isn't fully ready at this point it shouldn't be used to read any data.
	Clone(ctx context.Context, vm *UtilityVM, cd *cloneData) error
}

// A struct to keep all the information that might be required during cloning process of
// a resource.
type cloneData struct {
	// Builder for the virtual machine document.
	builder vm.UVMBuilder
	// scratchFolder of the clone
	scratchFolder string
	// UVMID of the clone
	uvmID string
}

// UVMTemplateConfig is just a wrapper struct that keeps together all the resources that
// need to be saved to create a template.
type UVMTemplateConfig struct {
	// ID of the template vm
	UVMID string
	// Array of all resources that will be required while making a clone from this template
	Resources []Cloneable
	// The OptionsWCOW used for template uvm creation
	CreateOpts OptionsWCOW
}

// Captures all the information that is necessary to properly save this UVM as a template
// and create clones from this template later. The struct returned by this method must be
// later on made available while creating a clone from this template.
func (uvm *UtilityVM) GenerateTemplateConfig() (*UVMTemplateConfig, error) {
	if _, ok := uvm.createOpts.(OptionsWCOW); !ok {
		return nil, fmt.Errorf("template config can only be created for a WCOW uvm")
	}

	// Add all the SCSI Mounts and VSMB shares into the list of clones
	templateConfig := &UVMTemplateConfig{
		UVMID:      uvm.ID(),
		CreateOpts: uvm.createOpts.(OptionsWCOW),
	}

	for _, vsmbShare := range uvm.vsmbDirShares {
		templateConfig.Resources = append(templateConfig.Resources, vsmbShare)
	}

	for _, vsmbShare := range uvm.vsmbFileShares {
		templateConfig.Resources = append(templateConfig.Resources, vsmbShare)
	}

	for _, location := range uvm.scsiLocations {
		for _, scsiMount := range location {
			if scsiMount != nil {
				templateConfig.Resources = append(templateConfig.Resources, scsiMount)
			}
		}
	}

	return templateConfig, nil
}

// Pauses the uvm and then saves it as a template. This uvm can not be restarted or used
// after it is successfully saved.
// uvm must be in the paused state before it can be saved as a template.save call will throw
// an incorrect uvm state exception if uvm is not in the paused state at the time of saving.
func (uvm *UtilityVM) SaveAsTemplate(ctx context.Context) error {
	if err := uvm.vm.Pause(ctx); err != nil {
		return errors.Wrap(err, "error pausing the VM")
	}
	if err := uvm.vm.Save(ctx); err != nil {
		return errors.Wrap(err, "error saving the VM")
	}
	return nil
}

// CloneContainer attaches back to a container that is already running inside the UVM
// because of the clone
func (uvm *UtilityVM) CloneContainer(ctx context.Context, id string) (cow.Container, error) {
	if uvm.gc == nil {
		return nil, fmt.Errorf("clone container cannot work without external GCS connection")
	}
	c, err := uvm.gc.CloneContainer(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to clone container %s: %s", id, err)
	}
	return c, nil
}
