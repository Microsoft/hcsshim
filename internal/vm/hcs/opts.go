package hcs

import (
	"context"

	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/pkg/errors"
)

// WithEnableCompartmentNamespace sets whether to enable namespacing the network compartment in the UVM
// for WCOW. Namespacing makes it so the compartment created for a container is essentially no longer
// aware or able to see any of the other compartments on the host (in this case the UVM).
func WithEnableCompartmentNamespace() vm.CreateOpt {
	return func(ctx context.Context, uvmb vm.UVMBuilder) error {
		builder, ok := uvmb.(*utilityVMBuilder)
		if !ok {
			return errors.New("object is not an hcs UVMBuilder")
		}
		// Here for a temporary workaround until the need for setting this regkey is no more. To protect
		// against any undesired behavior (such as some general networking scenarios ceasing to function)
		// with a recent change to fix SMB share access in the UVM, this registry key will be checked to
		// enable the change in question inside GNS.dll.
		builder.doc.VirtualMachine.RegistryChanges = &hcsschema.RegistryChanges{
			AddValues: []hcsschema.RegistryValue{
				{
					Key: &hcsschema.RegistryKey{
						Hive: "System",
						Name: "CurrentControlSet\\Services\\gns",
					},
					Name:       "EnableCompartmentNamespace",
					DWordValue: 1,
					Type_:      "DWord",
				},
			},
		}
		return nil
	}
}

// WithCloneConfig sets the necessary options for a cloneable Utility VM.
func WithCloneConfig(templateID string) vm.CreateOpt {
	return func(ctx context.Context, uvmb vm.UVMBuilder) error {
		builder, ok := uvmb.(*utilityVMBuilder)
		if !ok {
			return errors.New("object is not an hcs UVMBuilder")
		}

		builder.doc.VirtualMachine.RestoreState = &hcsschema.RestoreState{
			TemplateSystemId: templateID,
		}
		return nil
	}
}
