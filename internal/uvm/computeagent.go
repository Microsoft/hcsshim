package uvm

import (
	"context"
	"strings"

	"github.com/Microsoft/go-winio"
	"github.com/Microsoft/hcsshim/internal/computeagent"
	"github.com/Microsoft/hcsshim/internal/hns"
	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/Microsoft/hcsshim/pkg/octtrpc"
	"github.com/containerd/ttrpc"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Microsoft/hcsshim/internal/log"
)

// This file holds the implementation of the Compute Agent service that is exposed for
// external network configuration.

const ComputeAgentAddrFmt = "\\\\.\\pipe\\computeagent-%s"

// computeAgent implements the ComputeAgent ttrpc service for adding and deleting NICs to a
// Utility VM.
type computeAgent struct {
	uvm *UtilityVM
}

var _ computeagent.ComputeAgentService = &computeAgent{}

// AddNIC will add a NIC to the computeagent services hosting UVM.
func (ca *computeAgent) AddNIC(ctx context.Context, req *computeagent.AddNICInternalRequest) (*computeagent.AddNICInternalResponse, error) {
	log.G(ctx).WithFields(logrus.Fields{
		"containerID": req.ContainerID,
		"endpointID":  req.EndpointName,
		"nicID":       req.NicID,
	}).Info("AddNIC request")

	if req.NicID == "" || req.EndpointName == "" {
		return nil, status.Error(codes.InvalidArgument, "received empty field in request")
	}

	endpoint, err := hns.GetHNSEndpointByName(req.EndpointName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get endpoint with name %q", req.EndpointName)
	}
	if err := ca.uvm.AddEndpointToNSWithID(ctx, endpoint.Namespace.ID, req.NicID, endpoint); err != nil {
		return nil, err
	}
	return &computeagent.AddNICInternalResponse{}, nil
}

// ModifyNIC will modify a NIC from the computeagent services hosting UVM.
func (ca *computeAgent) ModifyNIC(ctx context.Context, req *computeagent.ModifyNICInternalRequest) (*computeagent.ModifyNICInternalResponse, error) {
	log.G(ctx).WithFields(logrus.Fields{
		"nicID":        req.NicID,
		"endpointName": req.EndpointName,
	}).Info("ModifyNIC request")

	if req.NicID == "" || req.EndpointName == "" || req.IovPolicySettings == nil {
		return nil, status.Error(codes.InvalidArgument, "received empty field in request")
	}

	endpoint, err := hns.GetHNSEndpointByName(req.EndpointName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get endpoint with name `%s`", req.EndpointName)
	}

	moderationValue := vm.InterruptModerationValue(req.IovPolicySettings.InterruptModeration)
	moderationName := vm.InterruptModerationValueToName[moderationValue]

	iovSettings := &vm.IovSettings{
		OffloadWeight:       &req.IovPolicySettings.IovOffloadWeight,
		QueuePairsRequested: &req.IovPolicySettings.QueuePairsRequested,
		InterruptModeration: &moderationName,
	}

	nic := &vm.NetworkAdapter{
		EndpointId:  endpoint.Id,
		MacAddress:  endpoint.MacAddress,
		IovSettings: iovSettings,
	}

	if err := ca.uvm.UpdateNIC(ctx, req.NicID, nic); err != nil {
		return nil, errors.Wrap(err, "failed to update UVM's network adapter")
	}

	return &computeagent.ModifyNICInternalResponse{}, nil
}

// DeleteNIC will delete a NIC from the computeagent services hosting UVM.
func (ca *computeAgent) DeleteNIC(ctx context.Context, req *computeagent.DeleteNICInternalRequest) (*computeagent.DeleteNICInternalResponse, error) {
	log.G(ctx).WithFields(logrus.Fields{
		"containerID":  req.ContainerID,
		"nicID":        req.NicID,
		"endpointName": req.EndpointName,
	}).Info("DeleteNIC request")

	if req.NicID == "" || req.EndpointName == "" {
		return nil, status.Error(codes.InvalidArgument, "received empty field in request")
	}

	endpoint, err := hns.GetHNSEndpointByName(req.EndpointName)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get endpoint with name %q", req.EndpointName)
	}

	if err := ca.uvm.RemoveEndpointFromNS(ctx, endpoint.Namespace.ID, endpoint); err != nil {
		return nil, err
	}
	return &computeagent.DeleteNICInternalResponse{}, nil
}

func setupAndServe(ctx context.Context, caAddr string, vm *UtilityVM) error {
	// Setup compute agent service
	l, err := winio.ListenPipe(caAddr, nil)
	if err != nil {
		return errors.Wrapf(err, "failed to listen on %s", caAddr)
	}
	s, err := ttrpc.NewServer(ttrpc.WithUnaryServerInterceptor(octtrpc.ServerInterceptor()))
	if err != nil {
		return err
	}
	computeagent.RegisterComputeAgentService(s, &computeAgent{vm})

	log.G(ctx).WithField("address", l.Addr().String()).Info("serving compute agent")
	go func() {
		defer l.Close()
		if err := trapClosedConnErr(s.Serve(ctx, l)); err != nil {
			log.G(ctx).WithError(err).Fatal("compute agent: serve failure")
		}
	}()

	return nil
}

func trapClosedConnErr(err error) error {
	if err == nil || strings.Contains(err.Error(), "use of closed network connection") {
		return nil
	}
	return err
}
