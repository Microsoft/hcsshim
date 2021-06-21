package uvm

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/Microsoft/hcsshim/internal/ncproxyttrpc"

	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/hcn"
	"github.com/Microsoft/hcsshim/internal/guestrequest"
	"github.com/Microsoft/hcsshim/internal/hcs/resourcepaths"
	hcsschema "github.com/Microsoft/hcsshim/internal/hcs/schema2"
	"github.com/Microsoft/hcsshim/internal/hns"
	"github.com/Microsoft/hcsshim/internal/log"
	"github.com/Microsoft/hcsshim/internal/requesttype"
	"github.com/Microsoft/hcsshim/osversion"
	"github.com/sirupsen/logrus"
)

var (
	// ErrNetNSAlreadyAttached is an error indicating the guest UVM already has
	// an endpoint by this id.
	ErrNetNSAlreadyAttached = errors.New("network namespace already added")
	// ErrNetNSNotFound is an error indicating the guest UVM does not have a
	// network namespace by this id.
	ErrNetNSNotFound = errors.New("network namespace not found")
	// ErrNICNotFound is an error indicating that the guest UVM does not have a NIC
	// by this id.
	ErrNICNotFound = errors.New("NIC not found in network namespace")
)

// Network namespace setup is a bit different for templates and clones.
// For templates and clones we use a special network namespace ID.
// Details about this can be found in the Networking section of the late-clone wiki page.
//
// In this function we take the namespace ID of the namespace that was created for this
// UVM. We hot add the namespace (with the default ID if this is a template). We get the
// endpoints associated with this namespace and then hot add those endpoints (by changing
// their namespace IDs by the deafult IDs if it is a template).
func (uvm *UtilityVM) SetupNetworkNamespace(ctx context.Context, nsid string) error {
	nsidInsideUVM := nsid
	if uvm.IsTemplate || uvm.IsClone {
		nsidInsideUVM = DEFAULT_CLONE_NETWORK_NAMESPACE_ID
	}

	// Query endpoints with actual nsid
	endpoints, err := GetNamespaceEndpoints(ctx, nsid)
	if err != nil {
		return err
	}

	// Add the network namespace inside the UVM if it is not a clone. (Clones will
	// inherit the namespace from template)
	if !uvm.IsClone {
		// Get the namespace struct from the actual nsid.
		hcnNamespace, err := hcn.GetNamespaceByID(nsid)
		if err != nil {
			return err
		}

		// All templates should have a special NSID so that it
		// will be easier to debug. Override it here.
		if uvm.IsTemplate {
			hcnNamespace.Id = nsidInsideUVM
		}

		if err = uvm.AddNetNS(ctx, hcnNamespace); err != nil {
			return err
		}
	}

	// If adding a network endpoint to clones or a template override nsid associated
	// with it.
	if uvm.IsClone || uvm.IsTemplate {
		// replace nsid for each endpoint
		for _, ep := range endpoints {
			ep.Namespace = &hns.Namespace{
				ID: nsidInsideUVM,
			}
		}
	}

	if err = uvm.AddEndpointsToNS(ctx, nsidInsideUVM, endpoints); err != nil {
		// Best effort clean up the NS
		if removeErr := uvm.RemoveNetNS(ctx, nsidInsideUVM); removeErr != nil {
			log.G(ctx).Warn(removeErr)
		}
		return err
	}
	return nil
}

// GetNamespaceEndpoints gets all endpoints in `netNS`
func GetNamespaceEndpoints(ctx context.Context, netNS string) ([]*hns.HNSEndpoint, error) {
	op := "uvm::GetNamespaceEndpoints"
	l := log.G(ctx).WithField("netns-id", netNS)
	l.Debug(op + " - Begin")
	defer func() {
		l.Debug(op + " - End")
	}()

	ids, err := hns.GetNamespaceEndpoints(netNS)
	if err != nil {
		return nil, err
	}
	var endpoints []*hns.HNSEndpoint
	for _, id := range ids {
		endpoint, err := hns.GetHNSEndpointByID(id)
		if err != nil {
			return nil, err
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, nil
}

// NCProxyEnabled returns if there is a network configuration client.
func (uvm *UtilityVM) NCProxyEnabled() bool {
	return uvm.ncProxyClient != nil
}

// NCProxyClient returns the network configuration proxy client.
func (uvm *UtilityVM) NCProxyClient() ncproxyttrpc.NetworkConfigProxyService {
	return uvm.ncProxyClient
}

// NetworkConfigType specifies the action to be performed during network configuration.
// For example: setup or teardown
type NetworkConfigType uint8

const (
	NetworkRequestSetup NetworkConfigType = iota
	NetworkRequestTearDown
)

var ErrNoNetworkSetup = errors.New("no network setup present for UVM")

// CreateAndAssignNetworkSetup creates and assigns a new NetworkSetup interface to the Utility VM.
// This can be used to configure the networking (setup and teardown) of the vm.
//
// `addr` is an optional parameter
func (uvm *UtilityVM) CreateAndAssignNetworkSetup(ctx context.Context, addr, containerID string) (err error) {
	if uvm.NCProxyEnabled() {
		if addr == "" || containerID == "" {
			return errors.New("received empty field(s) for external network setup")
		}
		setup, err := NewExternalNetworkSetup(ctx, uvm, addr, containerID)
		if err != nil {
			return err
		}
		uvm.networkSetup = setup
	} else {
		uvm.networkSetup = NewInternalNetworkSetup(uvm)
	}
	return nil
}

// ConfigureNetworking configures the utility VMs networking setup using the namespace ID
// `nsid`.
func (uvm *UtilityVM) ConfigureNetworking(ctx context.Context, nsid string) error {
	if uvm.networkSetup != nil {
		return uvm.networkSetup.ConfigureNetworking(ctx, nsid, NetworkRequestSetup)
	}
	return ErrNoNetworkSetup
}

// TearDownNetworking tears down the utility VMs networking setup using the namespace ID
// `nsid`.
func (uvm *UtilityVM) TearDownNetworking(ctx context.Context, nsid string) error {
	if uvm.networkSetup != nil {
		return uvm.networkSetup.ConfigureNetworking(ctx, nsid, NetworkRequestTearDown)
	}
	return ErrNoNetworkSetup
}

// NetworkSetup is used to abstract away the details of setting up networking
// for a container.
type NetworkSetup interface {
	ConfigureNetworking(ctx context.Context, namespaceID string, configType NetworkConfigType) error
}

// LocalNetworkSetup implements the NetworkSetup interface for configuring container
// networking.
type internalNetworkSetup struct {
	vm *UtilityVM
}

func NewInternalNetworkSetup(vm *UtilityVM) NetworkSetup {
	return &internalNetworkSetup{vm}
}

func (i *internalNetworkSetup) ConfigureNetworking(ctx context.Context, namespaceID string, configType NetworkConfigType) error {
	switch configType {
	case NetworkRequestSetup:
		if err := i.vm.SetupNetworkNamespace(ctx, namespaceID); err != nil {
			return err
		}
	case NetworkRequestTearDown:
		if err := i.vm.RemoveNetNS(ctx, namespaceID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("network configuration type %d is not known", configType)
	}

	return nil
}

// ExternalNetworkSetup implements the NetworkSetup interface for configuring
// container networking. It will try and communicate with an external network configuration
// proxy service to setup networking.
type externalNetworkSetup struct {
	vm          *UtilityVM
	caAddr      string
	containerID string
}

// NewExternalNetworkSetup returns an object implementing the NetworkSetup interface to be
// used for external network configuration.
func NewExternalNetworkSetup(ctx context.Context, vm *UtilityVM, caAddr, containerID string) (NetworkSetup, error) {
	if err := setupAndServe(ctx, caAddr, vm); err != nil {
		return nil, err
	}

	return &externalNetworkSetup{
		vm,
		caAddr,
		containerID,
	}, nil
}

func (e *externalNetworkSetup) ConfigureNetworking(ctx context.Context, namespaceID string, configType NetworkConfigType) error {
	client := e.vm.NCProxyClient()
	if client == nil {
		return fmt.Errorf("no ncproxy client for UVM %q", e.vm.ID())
	}

	netReq := &ncproxyttrpc.ConfigureNetworkingInternalRequest{
		ContainerID: e.containerID,
	}

	switch configType {
	case NetworkRequestSetup:
		if err := e.vm.AddNetNSByID(ctx, namespaceID); err != nil {
			return err
		}

		registerReq := &ncproxyttrpc.RegisterComputeAgentRequest{
			ContainerID:  e.containerID,
			AgentAddress: e.caAddr,
		}
		if _, err := client.RegisterComputeAgent(ctx, registerReq); err != nil {
			return err
		}

		netReq.RequestType = ncproxyttrpc.RequestTypeInternal_Setup
		if _, err := client.ConfigureNetworking(ctx, netReq); err != nil {
			return err
		}
	case NetworkRequestTearDown:
		netReq.RequestType = ncproxyttrpc.RequestTypeInternal_Teardown
		if _, err := client.ConfigureNetworking(ctx, netReq); err != nil {
			return err
		}
	default:
		return fmt.Errorf("network configuration type %d is not known", configType)
	}

	return nil
}

// NetworkEndpoints is a struct containing all of the endpoint IDs of a network
// namespace.
type NetworkEndpoints struct {
	EndpointIDs []string
	// ID of the namespace the endpoints belong to
	Namespace string
}

// Release releases the resources for all of the network endpoints in a namespace.
func (endpoints *NetworkEndpoints) Release(ctx context.Context) error {
	for _, endpoint := range endpoints.EndpointIDs {
		err := hns.RemoveNamespaceEndpoint(endpoints.Namespace, endpoint)
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			log.G(ctx).WithFields(logrus.Fields{
				"endpointID": endpoint,
				"netID":      endpoints.Namespace,
			}).Warn("removing endpoint from namespace: does not exist")
		}
	}
	endpoints.EndpointIDs = nil
	err := hns.RemoveNamespace(endpoints.Namespace)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// AddNetNS adds network namespace inside the guest without actually querying for the
// namespace by its ID. It uses the given namespace struct as it is in the guest request.
// This function is mostly used when we need to override the values inside the namespace
// struct returned by the GetNamespaceByID. For most uses cases AddNetNSByID is more appropriate.
//
// If a namespace with the same id already exists this returns `ErrNetNSAlreadyAttached`.
func (uvm *UtilityVM) AddNetNS(ctx context.Context, hcnNamespace *hcn.HostComputeNamespace) error {
	uvm.m.Lock()
	defer uvm.m.Unlock()
	if _, ok := uvm.namespaces[hcnNamespace.Id]; ok {
		return ErrNetNSAlreadyAttached
	}

	if uvm.isNetworkNamespaceSupported() {
		// Add a Guest Network namespace. On LCOW we add the adapters
		// dynamically.
		if uvm.operatingSystem == "windows" {
			guestNamespace := hcsschema.ModifySettingRequest{
				GuestRequest: guestrequest.GuestRequest{
					ResourceType: guestrequest.ResourceTypeNetworkNamespace,
					RequestType:  requesttype.Add,
					Settings:     hcnNamespace,
				},
			}
			if err := uvm.modify(ctx, &guestNamespace); err != nil {
				return err
			}
		}
	}

	if uvm.namespaces == nil {
		uvm.namespaces = make(map[string]*namespaceInfo)
	}
	uvm.namespaces[hcnNamespace.Id] = &namespaceInfo{
		nics: make(map[string]*nicInfo),
	}
	return nil
}

// AddNetNSByID adds finds the namespace with given `id` and adds that
// network namespace inside the guest.
//
// If a namespace with `id` already exists returns `ErrNetNSAlreadyAttached`.
func (uvm *UtilityVM) AddNetNSByID(ctx context.Context, id string) error {
	hcnNamespace, err := hcn.GetNamespaceByID(id)
	if err != nil {
		return err
	}

	if err = uvm.AddNetNS(ctx, hcnNamespace); err != nil {
		return err
	}
	return nil
}

// AddEndpointToNSWithID adds an endpoint to the network namespace with the specified
// NIC ID. If nicID is an empty string, a GUID will be generated for the ID instead.
//
// If no network namespace matches `id` returns `ErrNetNSNotFound`.
func (uvm *UtilityVM) AddEndpointToNSWithID(ctx context.Context, nsID, nicID string, endpoint *hns.HNSEndpoint) error {
	uvm.m.Lock()
	defer uvm.m.Unlock()
	ns, ok := uvm.namespaces[nsID]
	if !ok {
		return ErrNetNSNotFound
	}
	if _, ok := ns.nics[endpoint.Id]; !ok {
		if nicID == "" {
			id, err := guid.NewV4()
			if err != nil {
				return err
			}
			nicID = id.String()
		}
		if err := uvm.addNIC(ctx, nicID, endpoint); err != nil {
			return err
		}
		ns.nics[endpoint.Id] = &nicInfo{
			ID:       nicID,
			Endpoint: endpoint,
		}
	}
	return nil
}

// AddEndpointsToNS adds all unique `endpoints` to the network namespace
// matching `id`. On failure does not roll back any previously successfully
// added endpoints.
//
// If no network namespace matches `id` returns `ErrNetNSNotFound`.
func (uvm *UtilityVM) AddEndpointsToNS(ctx context.Context, id string, endpoints []*hns.HNSEndpoint) error {
	uvm.m.Lock()
	defer uvm.m.Unlock()

	ns, ok := uvm.namespaces[id]
	if !ok {
		return ErrNetNSNotFound
	}

	for _, endpoint := range endpoints {
		if _, ok := ns.nics[endpoint.Id]; !ok {
			nicID, err := guid.NewV4()
			if err != nil {
				return err
			}
			if err := uvm.addNIC(ctx, nicID.String(), endpoint); err != nil {
				return err
			}
			ns.nics[endpoint.Id] = &nicInfo{
				ID:       nicID.String(),
				Endpoint: endpoint,
			}
		}
	}
	return nil
}

// RemoveNetNS removes the namespace from the uvm and all remaining endpoints in
// the namespace.
//
// If a namespace matching `id` is not found this command silently succeeds.
func (uvm *UtilityVM) RemoveNetNS(ctx context.Context, id string) error {
	uvm.m.Lock()
	defer uvm.m.Unlock()
	if ns, ok := uvm.namespaces[id]; ok {
		for _, ninfo := range ns.nics {
			if err := uvm.removeNIC(ctx, ninfo.ID, ninfo.Endpoint); err != nil {
				return err
			}
			ns.nics[ninfo.Endpoint.Id] = nil
		}
		// Remove the Guest Network namespace
		if uvm.isNetworkNamespaceSupported() {
			if uvm.operatingSystem == "windows" {
				hcnNamespace, err := hcn.GetNamespaceByID(id)
				if err != nil {
					return err
				}
				guestNamespace := hcsschema.ModifySettingRequest{
					GuestRequest: guestrequest.GuestRequest{
						ResourceType: guestrequest.ResourceTypeNetworkNamespace,
						RequestType:  requesttype.Remove,
						Settings:     hcnNamespace,
					},
				}
				if err := uvm.modify(ctx, &guestNamespace); err != nil {
					return err
				}
			}
		}
		delete(uvm.namespaces, id)
	}
	return nil
}

// RemoveEndpointsFromNS removes all matching `endpoints` in the network
// namespace matching `id`. If no endpoint matching `endpoint.Id` is found in
// the network namespace this command silently succeeds.
//
// If no network namespace matches `id` returns `ErrNetNSNotFound`.
func (uvm *UtilityVM) RemoveEndpointsFromNS(ctx context.Context, id string, endpoints []*hns.HNSEndpoint) error {
	uvm.m.Lock()
	defer uvm.m.Unlock()

	ns, ok := uvm.namespaces[id]
	if !ok {
		return ErrNetNSNotFound
	}

	for _, endpoint := range endpoints {
		if ninfo, ok := ns.nics[endpoint.Id]; ok && ninfo != nil {
			if err := uvm.removeNIC(ctx, ninfo.ID, ninfo.Endpoint); err != nil {
				return err
			}
			delete(ns.nics, endpoint.Id)
		}
	}
	return nil
}

// RemoveEndpointFromNS removes ``endpoint` in the network
// namespace matching `id`. If no endpoint matching `endpoint.Id` is found in
// the network namespace this command returns `ErrNICNotFound`.
//
// If no network namespace matches `id` this function returns `ErrNetNSNotFound`.
func (uvm *UtilityVM) RemoveEndpointFromNS(ctx context.Context, id string, endpoint *hns.HNSEndpoint) error {
	uvm.m.Lock()
	defer uvm.m.Unlock()

	ns, ok := uvm.namespaces[id]
	if !ok {
		return ErrNetNSNotFound
	}

	if ninfo, ok := ns.nics[endpoint.Id]; ok && ninfo != nil {
		if err := uvm.removeNIC(ctx, ninfo.ID, ninfo.Endpoint); err != nil {
			return err
		}
		delete(ns.nics, endpoint.Id)
	} else {
		return ErrNICNotFound
	}
	return nil
}

// IsNetworkNamespaceSupported returns bool value specifying if network namespace is supported inside the guest
func (uvm *UtilityVM) isNetworkNamespaceSupported() bool {
	return uvm.guestCaps.NamespaceAddRequestSupported
}

func getNetworkModifyRequest(adapterID string, requestType string, settings interface{}) interface{} {
	if osversion.Build() >= osversion.RS5 {
		return guestrequest.NetworkModifyRequest{
			AdapterId:   adapterID,
			RequestType: requestType,
			Settings:    settings,
		}
	}
	return guestrequest.RS4NetworkModifyRequest{
		AdapterInstanceId: adapterID,
		RequestType:       requestType,
		Settings:          settings,
	}
}

// addNIC adds a nic to the Utility VM.
func (uvm *UtilityVM) addNIC(ctx context.Context, id string, endpoint *hns.HNSEndpoint) error {
	// First a pre-add. This is a guest-only request and is only done on Windows.
	if uvm.operatingSystem == "windows" {
		preAddRequest := hcsschema.ModifySettingRequest{
			GuestRequest: guestrequest.GuestRequest{
				ResourceType: guestrequest.ResourceTypeNetwork,
				RequestType:  requesttype.Add,
				Settings: getNetworkModifyRequest(
					id,
					requesttype.PreAdd,
					endpoint),
			},
		}
		if err := uvm.modify(ctx, &preAddRequest); err != nil {
			return err
		}
	}

	// Then the Add itself
	request := hcsschema.ModifySettingRequest{
		RequestType:  requesttype.Add,
		ResourcePath: fmt.Sprintf(resourcepaths.NetworkResourceFormat, id),
		Settings: hcsschema.NetworkAdapter{
			EndpointId: endpoint.Id,
			MacAddress: endpoint.MacAddress,
		},
	}

	if uvm.operatingSystem == "windows" {
		request.GuestRequest = guestrequest.GuestRequest{
			ResourceType: guestrequest.ResourceTypeNetwork,
			RequestType:  requesttype.Add,
			Settings: getNetworkModifyRequest(
				id,
				requesttype.Add,
				nil),
		}
	} else {
		// Verify this version of LCOW supports Network HotAdd
		if uvm.isNetworkNamespaceSupported() {
			request.GuestRequest = guestrequest.GuestRequest{
				ResourceType: guestrequest.ResourceTypeNetwork,
				RequestType:  requesttype.Add,
				Settings: &guestrequest.LCOWNetworkAdapter{
					NamespaceID:     endpoint.Namespace.ID,
					ID:              id,
					MacAddress:      endpoint.MacAddress,
					IPAddress:       endpoint.IPAddress.String(),
					PrefixLength:    endpoint.PrefixLength,
					GatewayAddress:  endpoint.GatewayAddress,
					DNSSuffix:       endpoint.DNSSuffix,
					DNSServerList:   endpoint.DNSServerList,
					EnableLowMetric: endpoint.EnableLowMetric,
					EncapOverhead:   endpoint.EncapOverhead,
				},
			}
		}
	}

	if err := uvm.modify(ctx, &request); err != nil {
		return err
	}

	return nil
}

// AddVFNIC only makes a request to setup interface inside the LCOW guest
func (uvm *UtilityVM) AddVFNIC(ctx context.Context, id string, cfg *guestrequest.LCOWNetworkAdapter) error {
	if !uvm.isNetworkNamespaceSupported() {
		return fmt.Errorf("guest does not support network namespaces and cannot add VF NIC %+v", cfg)
	}
	request := hcsschema.ModifySettingRequest{}
	request.GuestRequest = guestrequest.GuestRequest{
		ResourceType: guestrequest.ResourceTypeNetwork,
		RequestType:  requesttype.Add,
		Settings:     cfg,
	}

	return uvm.modify(ctx, &request)
}

func (uvm *UtilityVM) RemoveVFNIC(ctx context.Context, cfg *guestrequest.LCOWNetworkAdapter) error {
	if !uvm.isNetworkNamespaceSupported() {
		return fmt.Errorf("guest does not support network namespaces and cannot remove VF NIC %+v", cfg)
	}
	request := hcsschema.ModifySettingRequest{}
	request.GuestRequest = guestrequest.GuestRequest{
		ResourceType: guestrequest.ResourceTypeNetwork,
		RequestType:  requesttype.Remove,
		Settings:     cfg,
	}

	return uvm.modify(ctx, &request)
}

func (uvm *UtilityVM) removeNIC(ctx context.Context, id string, endpoint *hns.HNSEndpoint) error {
	request := hcsschema.ModifySettingRequest{
		RequestType:  requesttype.Remove,
		ResourcePath: fmt.Sprintf(resourcepaths.NetworkResourceFormat, id),
		Settings: hcsschema.NetworkAdapter{
			EndpointId: endpoint.Id,
			MacAddress: endpoint.MacAddress,
		},
	}

	if uvm.operatingSystem == "windows" {
		request.GuestRequest = hcsschema.ModifySettingRequest{
			RequestType: requesttype.Remove,
			Settings: getNetworkModifyRequest(
				id,
				requesttype.Remove,
				nil),
		}
	} else {
		// Verify this version of LCOW supports Network HotRemove
		if uvm.isNetworkNamespaceSupported() {
			request.GuestRequest = guestrequest.GuestRequest{
				ResourceType: guestrequest.ResourceTypeNetwork,
				RequestType:  requesttype.Remove,
				Settings: &guestrequest.LCOWNetworkAdapter{
					NamespaceID: endpoint.Namespace.ID,
					ID:          endpoint.Id,
				},
			}
		}
	}

	if err := uvm.modify(ctx, &request); err != nil {
		return err
	}
	return nil
}

// Removes all NICs added to this uvm.
func (uvm *UtilityVM) RemoveAllNICs(ctx context.Context) error {
	for _, ns := range uvm.namespaces {
		for _, ninfo := range ns.nics {
			if err := uvm.removeNIC(ctx, ninfo.ID, ninfo.Endpoint); err != nil {
				return err
			}
		}
	}
	return nil
}

// UpdateNIC updates a UVM's network adapter.
func (uvm *UtilityVM) UpdateNIC(ctx context.Context, id string, settings *hcsschema.NetworkAdapter) error {
	req := &hcsschema.ModifySettingRequest{
		RequestType:  requesttype.Update,
		ResourcePath: fmt.Sprintf(resourcepaths.NetworkResourceFormat, id),
		Settings:     settings,
	}
	return uvm.modify(ctx, req)
}
