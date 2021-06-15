package uvm

import (
	"context"

	"github.com/pkg/errors"
)

// GuestRequest sends an arbitrary guest request to the UVM.
func (uvm *UtilityVM) GuestRequest(ctx context.Context, guestReq interface{}) error {
	if err := uvm.gc.Modify(ctx, guestReq); err != nil {
		return errors.Wrap(err, "guest modify request failed")
	}
	return nil
}
