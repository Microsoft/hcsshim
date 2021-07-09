package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/cmd/ncproxy/ncproxygrpc"
	"github.com/Microsoft/hcsshim/cmd/ncproxy/nodenetsvc"
	"github.com/Microsoft/hcsshim/hcn"
	"github.com/Microsoft/hcsshim/internal/log"
	"google.golang.org/grpc"
)

// This is a barebones example of an implementation of the network
// config agent service that ncproxy talks to. This is solely used to test and
// will be removed.

var (
	configPath = flag.String("config", "", "Path to JSON configuration file.")

	prefixLength           = "24"
	prefixLengthInt uint32 = 24
	ipVersion              = "4"
)

func generateMAC() (string, error) {
	buf := make([]byte, 6)

	_, err := rand.Read(buf)
	if err != nil {
		return "", err
	}

	// set first number to 0
	buf[0] = 0
	mac := net.HardwareAddr(buf)
	macString := strings.ToUpper(mac.String())
	macString = strings.Replace(macString, ":", "-", -1)

	return macString, nil
}

func generateIPs(prefixLength string) (string, string, string) {
	buf := []byte{192, 168, 50}

	// set last to 0 for prefix
	ipPrefixBytes := append(buf, 0)
	ipPrefix := net.IP(ipPrefixBytes)
	ipPrefixString := ipPrefix.String() + "/" + prefixLength

	// set the last to 1 for gateway
	ipGatewayBytes := append(buf, 1)
	ipGateway := net.IP(ipGatewayBytes)
	ipGatewayString := ipGateway.String()

	// set last byte for IP address in range
	last := byte(rand.Intn(255-2) + 2)
	ipBytes := append(buf, last)
	ip := net.IP(ipBytes)
	ipString := ip.String()

	return ipPrefixString, ipGatewayString, ipString
}

func (s *service) addVFHelper(ctx context.Context, containerID, namespaceID string) (_ *nodenetsvc.ConfigureNetworkingResponse, err error) {
	_, gatewayIP, midIP := generateIPs(prefixLength)
	assignReq := &ncproxygrpc.AssignVFRequest{
		ContainerID:          containerID,
		DeviceID:             s.conf.NetworkingSettings.NICWithVFSettings.ID,
		VirtualFunctionIndex: s.conf.NetworkingSettings.NICWithVFSettings.VirtualFunctionIndex,
	}
	assignResp, err := s.client.AssignVF(ctx, assignReq)
	if err != nil {
		return nil, err
	}

	defer func() {
		if err != nil {
			// remove VF on failure
			removeReq := &ncproxygrpc.RemoveVFRequest{
				ContainerID:          containerID,
				DeviceID:             s.conf.NetworkingSettings.NICWithVFSettings.ID,
				VirtualFunctionIndex: s.conf.NetworkingSettings.NICWithVFSettings.VirtualFunctionIndex,
			}
			if _, err := s.client.RemoveVF(ctx, removeReq); err != nil {
				log.G(ctx).WithError(err).Info("Failed to remove VF")
			}
		}
	}()

	addReq := &ncproxygrpc.AddNICWithVFRequest{
		NamespaceID:           namespaceID,
		ContainerID:           containerID,
		NicID:                 assignResp.ID,
		Ipaddress:             midIP,
		IpaddressPrefixlength: prefixLengthInt,
		DefaultGateway:        gatewayIP,
	}

	_, err = s.client.AddNICWithVF(ctx, addReq)
	if err != nil {
		return nil, err
	}

	s.containerToNICVirtualFunctionID[containerID] = assignResp.ID
	return &nodenetsvc.ConfigureNetworkingResponse{}, nil
}

func (s *service) ConfigureContainerNetworking(ctx context.Context, req *nodenetsvc.ConfigureContainerNetworkingRequest) (_ *nodenetsvc.ConfigureContainerNetworkingResponse, err error) {
	// for testing purposes, make the endpoint here
	log.G(ctx).WithField("req", req).Info("ConfigureContainreNetworking request")

	if req.RequestType == nodenetsvc.RequestType_Setup {
		prefixIP, gatewayIP, midIP := generateIPs(prefixLength)

		addNetworkReq := &ncproxygrpc.CreateNetworkRequest{
			Name:                  req.ContainerID + "_network",
			Mode:                  ncproxygrpc.CreateNetworkRequest_Transparent,
			SwitchName:            s.conf.NetworkingSettings.HNSSettings.SwitchName,
			IpamType:              ncproxygrpc.CreateNetworkRequest_Static,
			SubnetIpaddressPrefix: []string{prefixIP},
			DefaultGateway:        gatewayIP,
		}

		networkResp, err := s.client.CreateNetwork(ctx, addNetworkReq)
		if err != nil {
			return nil, err
		}

		network, err := hcn.GetNetworkByID(networkResp.ID)
		if err != nil {
			return nil, err
		}
		s.containerToNetwork[req.ContainerID] = network.Name

		mac, err := generateMAC()
		if err != nil {
			return nil, err
		}

		name := req.ContainerID + "_endpoint"
		endpointCreateReq := &ncproxygrpc.CreateEndpointRequest{
			Name:                  name,
			Macaddress:            mac,
			Ipaddress:             midIP,
			IpaddressPrefixlength: prefixLength,
			NetworkName:           network.Name,
			IovPolicySettings:     s.conf.NetworkingSettings.HNSSettings.IOVSettings,
		}

		endpt, err := s.client.CreateEndpoint(ctx, endpointCreateReq)
		if err != nil {
			return nil, err
		}

		log.G(ctx).WithField("endpt", endpt).Info("ConfigureContainreNetworking created endpoint")

		addEndpointReq := &ncproxygrpc.AddEndpointRequest{
			Name:        name,
			NamespaceID: req.NetworkNamespaceID,
		}
		_, err = s.client.AddEndpoint(ctx, addEndpointReq)
		if err != nil {
			return nil, err
		}
		s.containerToNamespace[req.ContainerID] = req.NetworkNamespaceID

		resultIPAddr := &nodenetsvc.ContainerIPAddress{
			Version:        ipVersion,
			Ip:             midIP,
			PrefixLength:   prefixLength,
			DefaultGateway: gatewayIP,
		}
		netInterface := &nodenetsvc.ContainerNetworkInterface{
			Name:               network.Name,
			MacAddress:         mac,
			NetworkNamespaceID: req.NetworkNamespaceID,
			Ipaddresses:        []*nodenetsvc.ContainerIPAddress{resultIPAddr},
		}

		return &nodenetsvc.ConfigureContainerNetworkingResponse{
			Interfaces: []*nodenetsvc.ContainerNetworkInterface{netInterface},
		}, nil
	} else if req.RequestType == nodenetsvc.RequestType_Teardown {
		eReq := &ncproxygrpc.GetEndpointsRequest{}
		resp, err := s.client.GetEndpoints(ctx, eReq)
		if err != nil {
			return nil, err
		}

		for _, endpoint := range resp.Endpoints {
			if endpoint.Namespace == req.NetworkNamespaceID {
				deleteEndptReq := &ncproxygrpc.DeleteEndpointRequest{
					Name: endpoint.Name,
				}
				if _, err := s.client.DeleteEndpoint(ctx, deleteEndptReq); err != nil {
					log.G(ctx).WithField("name", endpoint.Name).Warn("failed to delete endpoint")
				}
			}
		}

		if networkName, ok := s.containerToNetwork[req.ContainerID]; ok {
			deleteReq := &ncproxygrpc.DeleteNetworkRequest{
				Name: networkName,
			}
			if _, err := s.client.DeleteNetwork(ctx, deleteReq); err != nil {
				log.G(ctx).WithField("name", networkName).Warn("failed to delete network")
			}
			delete(s.containerToNetwork, req.ContainerID)
		}

		return &nodenetsvc.ConfigureContainerNetworkingResponse{}, nil

	}
	return nil, fmt.Errorf("invalid request type %v", req.RequestType)
}

func (s *service) addHelper(ctx context.Context, req *nodenetsvc.ConfigureNetworkingRequest, containerNamespaceID string) (_ *nodenetsvc.ConfigureNetworkingResponse, err error) {
	if s.conf.NetworkingSettings != nil && s.conf.NetworkingSettings.NICWithVFSettings != nil {
		return s.addVFHelper(ctx, req.ContainerID, containerNamespaceID)
	}
	return s.addHNSHelper(ctx, req, containerNamespaceID)
}

func (s *service) addHNSHelper(ctx context.Context, req *nodenetsvc.ConfigureNetworkingRequest, containerNamespaceID string) (_ *nodenetsvc.ConfigureNetworkingResponse, err error) {
	eReq := &ncproxygrpc.GetEndpointsRequest{}
	resp, err := s.client.GetEndpoints(ctx, eReq)
	if err != nil {
		return nil, err
	}
	log.G(ctx).WithField("endpts", resp.Endpoints).Info("ConfigureNetworking addrequest")

	for _, endpoint := range resp.Endpoints {
		if endpoint.Namespace == containerNamespaceID {
			// add endpoints that are in the namespace as NICs
			nicID, err := guid.NewV4()
			if err != nil {
				return nil, fmt.Errorf("failed to create nic GUID: %s", err)
			}
			nsReq := &ncproxygrpc.AddNICRequest{
				ContainerID:  req.ContainerID,
				NicID:        nicID.String(),
				EndpointName: endpoint.Name,
			}
			if _, err := s.client.AddNIC(ctx, nsReq); err != nil {
				return nil, err
			}
			s.endpointToNicID[endpoint.Name] = nicID.String()
		}

	}

	defer func() {
		if err != nil {
			_, _ = s.teardownHelper(ctx, req, containerNamespaceID)
		}
	}()

	return &nodenetsvc.ConfigureNetworkingResponse{}, nil

}

func (s *service) teardownHelper(ctx context.Context, req *nodenetsvc.ConfigureNetworkingRequest, containerNamespaceID string) (*nodenetsvc.ConfigureNetworkingResponse, error) {
	if s.conf.NetworkingSettings != nil && s.conf.NetworkingSettings.NICWithVFSettings != nil {
		return s.teardownVFHelper(ctx, req, containerNamespaceID)
	}
	return s.teardownHNSHelper(ctx, req, containerNamespaceID)
}

func (s *service) teardownVFHelper(ctx context.Context, req *nodenetsvc.ConfigureNetworkingRequest, containerNamespaceID string) (*nodenetsvc.ConfigureNetworkingResponse, error) {
	// delete the interface from the container namespace
	nicID, ok := s.containerToNICVirtualFunctionID[req.ContainerID]
	if !ok {
		return nil, fmt.Errorf("there is no nic VF configured for container %s", req.ContainerID)
	}
	deleteReq := &ncproxygrpc.DeleteNICWithVFRequest{
		ContainerID: req.ContainerID,
		NicID:       nicID,
	}
	if _, err := s.client.DeleteNICWithVF(ctx, deleteReq); err != nil {
		// best effort
		log.G(ctx).WithField("nicID", nicID).Warn("failed to delete endpoint nic")
	}

	// then remove the VF from the UVM
	removeVFReq := &ncproxygrpc.RemoveVFRequest{
		ContainerID:          req.ContainerID,
		DeviceID:             nicID,
		VirtualFunctionIndex: s.conf.NetworkingSettings.NICWithVFSettings.VirtualFunctionIndex,
	}
	if _, err := s.client.RemoveVF(ctx, removeVFReq); err != nil {
		return nil, err
	}

	delete(s.containerToNICVirtualFunctionID, req.ContainerID)
	return &nodenetsvc.ConfigureNetworkingResponse{}, nil
}

func (s *service) teardownHNSHelper(ctx context.Context, req *nodenetsvc.ConfigureNetworkingRequest, containerNamespaceID string) (*nodenetsvc.ConfigureNetworkingResponse, error) {
	eReq := &ncproxygrpc.GetEndpointsRequest{}
	resp, err := s.client.GetEndpoints(ctx, eReq)
	if err != nil {
		return nil, err
	}
	for _, endpoint := range resp.Endpoints {
		if endpoint.Namespace == containerNamespaceID {
			nicID, ok := s.endpointToNicID[endpoint.Name]
			if !ok {
				log.G(ctx).WithField("name", endpoint.Name).Warn("endpoint was not assigned a NIC ID previously")
				continue
			}
			// remove endpoints that are in the namespace as NICs
			nsReq := &ncproxygrpc.DeleteNICRequest{
				ContainerID:  req.ContainerID,
				NicID:        nicID,
				EndpointName: endpoint.Name,
			}
			if _, err := s.client.DeleteNIC(ctx, nsReq); err != nil {
				log.G(ctx).WithField("name", endpoint.Name).Warn("failed to delete endpoint nic")
			}
			delete(s.endpointToNicID, endpoint.Name)
		}
	}
	return &nodenetsvc.ConfigureNetworkingResponse{}, nil
}

func (s *service) modifyHelper(ctx context.Context, containerID, containerNamespaceID string, iovSettings *ncproxygrpc.IovEndpointPolicySetting) error {
	eReq := &ncproxygrpc.GetEndpointsRequest{}
	resp, err := s.client.GetEndpoints(ctx, eReq)
	if err != nil {
		return err
	}
	for _, endpoint := range resp.Endpoints {
		if endpoint.Namespace == containerNamespaceID {
			nicID, ok := s.endpointToNicID[endpoint.Name]
			if !ok {
				return fmt.Errorf("endpoint was not assigned a NIC ID previously")
			}

			req := &ncproxygrpc.ModifyNICRequest{
				ContainerID:       containerID,
				NicID:             nicID,
				EndpointName:      endpoint.Name,
				IovPolicySettings: iovSettings,
			}
			if _, err := s.client.ModifyNIC(ctx, req); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *service) ConfigureNetworking(ctx context.Context, req *nodenetsvc.ConfigureNetworkingRequest) (*nodenetsvc.ConfigureNetworkingResponse, error) {
	containerNamespaceID, ok := s.containerToNamespace[req.ContainerID]
	if !ok {
		return nil, fmt.Errorf("no namespace was previously created for containerID %s", req.ContainerID)
	}

	log.G(ctx).WithField("req", req).Info("ConfigureNetworking request")
	if req.RequestType == nodenetsvc.RequestType_Setup {
		return s.addHelper(ctx, req, containerNamespaceID)
	}
	return s.teardownHelper(ctx, req, containerNamespaceID)
}

func (s *service) PingNodeNetworkService(ctx context.Context, req *nodenetsvc.PingNodeNetworkServiceRequest) (*nodenetsvc.PingNodeNetworkServiceResponse, error) {
	return &nodenetsvc.PingNodeNetworkServiceResponse{}, nil
}

func main() {
	var err error
	ctx := context.Background()

	flag.Parse()
	conf, err := readConfig(*configPath)
	if err != nil {
		log.G(ctx).WithError(err).Errorf("failed to read network agent's config file at %s", *configPath)
		os.Exit(1)
	}
	log.G(ctx).WithField("conf path", *configPath).Info("network agent conf path")

	log.G(ctx).WithField("conf", conf).Info("network agent conf contents")

	sigChan := make(chan os.Signal, 1)
	serveErr := make(chan error, 1)
	defer close(serveErr)
	signal.Notify(sigChan, syscall.SIGINT)
	defer signal.Stop(sigChan)

	grpcClient, err := grpc.Dial(
		conf.GRPCAddr,
		grpc.WithInsecure(),
	)
	if err != nil {
		log.G(ctx).WithError(err).Errorf("failed to connect to ncproxy at %s", conf.GRPCAddr)
		os.Exit(1)
	}
	defer grpcClient.Close()

	log.G(ctx).WithField("addr", conf.GRPCAddr).Info("connected to ncproxy")
	ncproxyClient := ncproxygrpc.NewNetworkConfigProxyClient(grpcClient)
	service := &service{
		conf:                            conf,
		client:                          ncproxyClient,
		containerToNamespace:            make(map[string]string),
		endpointToNicID:                 make(map[string]string),
		containerToNetwork:              make(map[string]string),
		containerToNICVirtualFunctionID: make(map[string]string),
	}
	server := grpc.NewServer()
	nodenetsvc.RegisterNodeNetworkServiceServer(server, service)

	grpcListener, err := net.Listen("tcp", conf.NodeNetSvcAddr)
	if err != nil {
		log.G(ctx).WithError(err).Errorf("failed to listen on %s", grpcListener.Addr().String())
		os.Exit(1)
	}

	go func() {
		defer grpcListener.Close()
		if err := server.Serve(grpcListener); err != nil {
			if strings.Contains(err.Error(), "use of closed network connection") {
				serveErr <- nil
			}
			serveErr <- err
		}
	}()

	log.G(ctx).WithField("addr", conf.NodeNetSvcAddr).Info("serving network service agent")

	// Wait for server error or user cancellation.
	select {
	case <-sigChan:
		log.G(ctx).Info("Received interrupt. Closing")
	case err := <-serveErr:
		if err != nil {
			log.G(ctx).WithError(err).Fatal("grpc service failure")
		}
	}

	// Cancel inflight requests and shutdown service
	server.GracefulStop()
}
