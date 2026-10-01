package timeclockstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
)

// LookupPunchReceipt returns the durable receipt used to short-circuit an
// exact retry before session or roster state is evaluated.
func (a Adapter) LookupPunchReceipt(ctx context.Context, tenant, device string, sequence int64) (clockservice.ReceiptRecord, bool, error) {
	if a.Store == nil {
		return clockservice.ReceiptRecord{}, false, clockservice.ErrUnavailable
	}
	row, found, err := a.Store.LookupPunchReceipt(ctx, tenant, device, sequence)
	if err != nil || !found {
		return clockservice.ReceiptRecord{}, found, err
	}
	return clockservice.ReceiptRecord{DeviceSequence: row.DeviceSequence, Status: row.Status, Reason: row.Reason, ObservationID: row.ObservationID, Payload: append([]byte(nil), row.Payload...)}, true, nil
}

var _ clockservice.BatchReceiptLookup = Adapter{}
