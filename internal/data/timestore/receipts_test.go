package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestTodo_TCLOCK_003_Integration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-device"
	device := "device-1"

	// Submit out of order with a gap at 3: 1,2,4,5 acknowledge but the
	// contiguous cursor stops at 2 until the gap is filled.
	batch := []ReceiptRow{
		{DeviceSequence: 2, Status: "ACCEPTED", Payload: json.RawMessage(`{"n":2}`)},
		{DeviceSequence: 1, Status: "ACCEPTED", Payload: json.RawMessage(`{"n":1}`)},
		{DeviceSequence: 5, Status: "HELD", Reason: "sequence gap", Payload: json.RawMessage(`{"n":5}`)},
		{DeviceSequence: 4, Status: "REJECTED", Reason: "bad worker", Payload: json.RawMessage(`{"n":4}`)},
	}
	receipts, highest, err := s.RecordBatch(ctx, tenant, device, batch)
	if err != nil {
		t.Fatalf("RecordBatch: %v", err)
	}
	if len(receipts) != 4 {
		t.Fatalf("receipts = %d", len(receipts))
	}
	if highest != 2 {
		t.Fatalf("highest contiguous = %d, want 2 (gap at 3)", highest)
	}

	cursor, err := s.DeviceCursor(ctx, tenant, device)
	if err != nil || cursor != 2 {
		t.Fatalf("DeviceCursor = %d, %v", cursor, err)
	}

	// Retrying the exact same batch returns the original receipts
	// unchanged rather than duplicating rows.
	retry, _, err := s.RecordBatch(ctx, tenant, device, batch)
	if err != nil {
		t.Fatalf("retry RecordBatch: %v", err)
	}
	for i := range retry {
		if retry[i].Status != receipts[i].Status {
			t.Fatalf("retried receipt %d changed status: %+v vs %+v", i, retry[i], receipts[i])
		}
	}

	// A duplicate device_sequence with a different status is still
	// resolved to the original receipt: the receipt, once issued, is
	// immutable evidence.
	changedDuplicate := []ReceiptRow{{DeviceSequence: 1, Status: "REJECTED", Reason: "changed mind"}}
	dupOut, _, err := s.RecordBatch(ctx, tenant, device, changedDuplicate)
	if err != nil || dupOut[0].Status != "ACCEPTED" {
		t.Fatalf("duplicate sequence should keep original receipt: %+v, %v", dupOut, err)
	}

	// Filling the gap at 3 advances the contiguous cursor through the
	// already-acknowledged 4 and 5.
	fill := []ReceiptRow{{DeviceSequence: 3, Status: "ACCEPTED", Payload: json.RawMessage(`{"n":3}`)}}
	_, highest2, err := s.RecordBatch(ctx, tenant, device, fill)
	if err != nil {
		t.Fatalf("RecordBatch fill: %v", err)
	}
	if highest2 != 5 {
		t.Fatalf("highest contiguous after fill = %d, want 5", highest2)
	}

	// Outbox events were written for every distinct receipt, in the same
	// transaction as the receipt itself.
	events, err := s.ListEvents(ctx, tenant, 0, 100)
	if err != nil || len(events) != 5 {
		t.Fatalf("outbox events = %d, %v", len(events), err)
	}

	// A second device on the same tenant starts its own independent cursor.
	otherHighest, err := s.DeviceCursor(ctx, tenant, "device-2")
	if err != nil || otherHighest != 0 {
		t.Fatalf("fresh device cursor = %d, %v", otherHighest, err)
	}

	// Input validation.
	if _, _, err := s.RecordBatch(ctx, tenant, device, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty batch: %v", err)
	}
	if _, _, err := s.RecordBatch(ctx, tenant, device, []ReceiptRow{{DeviceSequence: 0, Status: "ACCEPTED"}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zero sequence: %v", err)
	}
}

func BenchmarkTodo_TCLOCK_003(b *testing.B) {
	// fixture needs a *testing.T; pgtest has no *testing.B entry point. A
	// zero-value T is safe here: its Cleanup never fires, but the whole
	// embedded server (and every schema on it, including this one) is torn
	// down by pgtest.RunMain's stopServer when the test binary exits.
	s := fixture(new(testing.T))
	ctx := context.Background()
	tenant := "tenant-bench"

	const batchSize = 200
	batch := make([]ReceiptRow, batchSize)
	for i := 0; i < batchSize; i++ {
		batch[i] = ReceiptRow{DeviceSequence: int64(i + 1), Status: "ACCEPTED", Payload: json.RawMessage(`{}`)}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		device := fmt.Sprintf("device-bench-%d", i)
		if _, _, err := s.RecordBatch(ctx, tenant, device, batch); err != nil {
			b.Fatalf("RecordBatch: %v", err)
		}
	}
}
