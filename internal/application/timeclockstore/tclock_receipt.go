package timeclockstore

import (
	"context"
	"encoding/json"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

// ReceiptAdapter maps per-device receipt batches to timestore.
type ReceiptAdapter struct{ Store *timestore.Store }

var _ clockservice.ReceiptStore = ReceiptAdapter{}

// RecordBatch stores receipts and returns the contiguous device cursor.
func (a ReceiptAdapter) RecordBatch(ctx context.Context, tenant, deviceID string, batch []clockservice.ReceiptRecord) ([]clockservice.ReceiptRecord, int64, error) {
	if a.Store == nil {
		return nil, 0, clockservice.ErrUnavailable
	}
	rows := make([]timestore.ReceiptRow, len(batch))
	for i, r := range batch {
		rows[i] = timestore.ReceiptRow{DeviceSequence: r.DeviceSequence, Status: r.Status, Reason: r.Reason, ObservationID: r.ObservationID, Payload: json.RawMessage(append([]byte(nil), r.Payload...))}
	}
	got, cursor, err := a.Store.RecordBatch(ctx, tenant, deviceID, rows)
	if err != nil {
		return nil, 0, err
	}
	out := make([]clockservice.ReceiptRecord, len(got))
	for i, r := range got {
		out[i] = clockservice.ReceiptRecord{Status: r.Status, Reason: r.Reason, ObservationID: r.ObservationID, DeviceSequence: r.DeviceSequence, Payload: append([]byte(nil), r.Payload...)}
	}
	return out, cursor, nil
}

// DeviceCursor returns the highest contiguous receipt sequence.

func (a ReceiptAdapter) DeviceCursor(ctx context.Context, tenant, deviceID string) (int64, error) {
	if a.Store == nil {
		return 0, clockservice.ErrUnavailable
	}
	return a.Store.DeviceCursor(ctx, tenant, deviceID)
}
