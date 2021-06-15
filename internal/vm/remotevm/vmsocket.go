package remotevm

import (
	"context"
	"io/ioutil"
	"net"
	"os"

	"github.com/Microsoft/go-winio/pkg/guid"
	"github.com/Microsoft/hcsshim/internal/vm"
	"github.com/Microsoft/hcsshim/internal/vmservice"
	"github.com/pkg/errors"
)

// Get a random unix socket address to use. The "randomness" equates to makes a temp file to reserve a name
// and then shortly after deleting it and using this as the socket address.
func randomUnixSockAddr() (string, error) {
	// Make a temp file and delete to "reserve" a unique name for the unix socket
	f, err := ioutil.TempFile("", "")
	if err != nil {
		return "", errors.Wrap(err, "failed to create temp file for unix socket")
	}

	if err := f.Close(); err != nil {
		return "", errors.Wrap(err, "failed to close temp file")
	}

	if err := os.Remove(f.Name()); err != nil {
		return "", errors.Wrap(err, "failed to delete temp file to free up name")
	}

	return f.Name(), nil
}

func (uvm *utilityVM) VMSocketListen(ctx context.Context, listenType vm.VMSocketType, connID interface{}) (_ net.Listener, err error) {
	addr, err := randomUnixSockAddr()
	if err != nil {
		return nil, err
	}

	l, err := net.Listen("unix", addr)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to listen on unix socket %q", addr)
	}

	defer func() {
		if err != nil {
			_ = l.Close()
		}
	}()

	switch listenType {
	case vm.HvSocket:
		serviceGUID, ok := connID.(guid.GUID)
		if !ok {
			return nil, errors.New("parameter passed to hvsocketlisten is not a GUID")
		}
		if err := uvm.hvSocketListen(ctx, serviceGUID.String(), addr); err != nil {
			return nil, errors.Wrap(err, "failed to setup relay to hvsocket listener")
		}
	case vm.VSock:
		port, ok := connID.(uint32)
		if !ok {
			return nil, errors.New("parameter passed to vsocklisten is not the right type")
		}
		if err := uvm.vsockListen(ctx, port, addr); err != nil {
			return nil, errors.Wrap(err, "failed to setup relay to vsock listener")
		}
	default:
		return nil, errors.New("unknown vmsocket type requested")
	}

	return l, nil
}

func (uvm *utilityVM) UpdateVMSocket(ctx context.Context, socketType vm.VMSocketType, sid string, serviceConfig *vm.HvSocketServiceConfig) error {
	return vm.ErrNotSupported
}

func (uvm *utilityVM) hvSocketListen(ctx context.Context, serviceID, listenerPath string) error {
	if _, err := uvm.client.VMSocket(ctx, &vmservice.VMSocketRequest{
		Type: vmservice.ModifyType_ADD,
		Config: &vmservice.VMSocketRequest_HvsocketList{
			HvsocketList: &vmservice.HVSocketListen{
				ServiceID:    serviceID,
				ListenerPath: listenerPath,
			},
		},
	}); err != nil {
		return err
	}
	return nil
}

func (uvm *utilityVM) vsockListen(ctx context.Context, port uint32, listenerPath string) error {
	if _, err := uvm.client.VMSocket(ctx, &vmservice.VMSocketRequest{
		Type: vmservice.ModifyType_ADD,
		Config: &vmservice.VMSocketRequest_VsockListen{
			VsockListen: &vmservice.VSockListen{
				Port:         port,
				ListenerPath: listenerPath,
			},
		},
	}); err != nil {
		return err
	}
	return nil
}
