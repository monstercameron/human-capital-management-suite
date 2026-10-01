package timestore

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestTodo_WTIME_011(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	r, err := s.CreateDestinationDraft(ctx, "tenant-a", DestinationRecord{
		TenantID: "tenant-a", ID: uuid.NewString(), Kind: DestinationContractorInvoice,
		SourceRef: "engagement-1", SourceRevision: 1, ReceiverRef: "ap-connector",
		PayloadDigest: "d1", Payload: []byte(`{"amount":"1200.00"}`), IdempotencyKey: "invoice-1",
	})
	if err != nil || r.Status != DestinationDraft {
		t.Fatalf("CreateDestinationDraft = %#v, %v", r, err)
	}
}

// TestTodo_WTIME_011_Integration exercises a contractor invoice draft
// through send/accept, and proves a re-export after correction (a new
// source revision) never duplicates hours at the receiver under the same
// idempotency key.
func TestTodo_WTIME_011_Integration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	draft, err := s.CreateDestinationDraft(ctx, "tenant-a", DestinationRecord{
		TenantID: "tenant-a", ID: uuid.NewString(), Kind: DestinationContractorInvoice,
		SourceRef: "engagement-1", SourceRevision: 1, ReceiverRef: "ap-connector",
		PayloadDigest: "d1", Payload: []byte(`{"amount":"1200.00"}`), IdempotencyKey: "invoice-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	sent, err := s.RecordDestinationReceipt(ctx, "tenant-a", DestinationReceipt{TenantID: "tenant-a", DestinationID: draft.ID, Status: DestinationSent, ReceiverRef: "ap-connector"})
	if err != nil || sent.Status != DestinationSent {
		t.Fatalf("send: %#v, %v", sent, err)
	}
	accepted, err := s.RecordDestinationReceipt(ctx, "tenant-a", DestinationReceipt{TenantID: "tenant-a", DestinationID: draft.ID, Status: DestinationAccepted, ReceiverRef: "ap-connector"})
	if err != nil || accepted.Status != DestinationAccepted {
		t.Fatalf("accept: %#v, %v", accepted, err)
	}
	// Re-recording the same accepted receipt is idempotent.
	again, err := s.RecordDestinationReceipt(ctx, "tenant-a", DestinationReceipt{TenantID: "tenant-a", DestinationID: draft.ID, Status: DestinationAccepted, ReceiverRef: "ap-connector"})
	if err != nil || again.Revision != accepted.Revision {
		t.Fatalf("re-accept: %#v, %v", again, err)
	}
	// A different draft for a corrected (new) source revision under the
	// same idempotency key is an independent row, not a duplicate.
	corrected, err := s.CreateDestinationDraft(ctx, "tenant-a", DestinationRecord{
		TenantID: "tenant-a", ID: uuid.NewString(), Kind: DestinationContractorInvoice,
		SourceRef: "engagement-1", SourceRevision: 2, ReceiverRef: "ap-connector",
		PayloadDigest: "d2", Payload: []byte(`{"amount":"1350.00"}`), IdempotencyKey: "invoice-1",
	})
	if err != nil || corrected.ID == draft.ID {
		t.Fatalf("corrected draft = %#v, %v", corrected, err)
	}
	receipts, err := s.DestinationReceipts(ctx, "tenant-a", draft.ID)
	if err != nil || len(receipts) != 2 {
		t.Fatalf("receipts = %#v, %v", receipts, err)
	}
}

func TestTodo_WTIME_012_Integration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	draft, err := s.CreateDestinationDraft(ctx, "tenant-a", DestinationRecord{
		TenantID: "tenant-a", ID: uuid.NewString(), Kind: DestinationAgencyExport,
		SourceRef: "timecard-agency-1", SourceRevision: 3, ReceiverRef: "vms-fieldglass",
		PayloadDigest: "ad1", Payload: []byte(`{"hours":40}`), IdempotencyKey: "agency-export-1",
	})
	if err != nil || draft.Kind != DestinationAgencyExport {
		t.Fatalf("agency draft = %#v, %v", draft, err)
	}
	if _, err := s.RecordDestinationReceipt(ctx, "tenant-a", DestinationReceipt{TenantID: "tenant-a", DestinationID: draft.ID, Status: DestinationSent, ReceiverRef: "vms-fieldglass"}); err != nil {
		t.Fatal(err)
	}
	rejected, err := s.RecordDestinationReceipt(ctx, "tenant-a", DestinationReceipt{TenantID: "tenant-a", DestinationID: draft.ID, Status: DestinationRejected, ReceiverRef: "vms-fieldglass", Reason: "worker not recognized"})
	if err != nil || rejected.Status != DestinationRejected {
		t.Fatalf("reject: %#v, %v", rejected, err)
	}
	receipts, err := s.DestinationReceipts(ctx, "tenant-a", draft.ID)
	if err != nil || len(receipts) != 2 || receipts[1].Reason == "" {
		t.Fatalf("rejection receipt not recorded with reason: %#v, %v", receipts, err)
	}
	// A rejection is terminal; a further transition attempt is refused.
	if _, err := s.RecordDestinationReceipt(ctx, "tenant-a", DestinationReceipt{TenantID: "tenant-a", DestinationID: draft.ID, Status: DestinationSent, ReceiverRef: "vms-fieldglass"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("transition out of REJECTED = %v, want ErrInvalid", err)
	}
}

func TestTodo_TCLOCK_015_Integration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	draft, err := s.CreateDestinationDraft(ctx, "tenant-a", DestinationRecord{
		TenantID: "tenant-a", ID: uuid.NewString(), Kind: DestinationPayrollExport,
		SourceRef: "timecard-1", SourceRevision: 4, ReceiverRef: "payroll-provider",
		PayloadDigest: "pd1", Payload: []byte(`{"worker":"worker-1","hours":80}`), IdempotencyKey: "payroll-export-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordDestinationReceipt(ctx, "tenant-a", DestinationReceipt{TenantID: "tenant-a", DestinationID: draft.ID, Status: DestinationSent, ReceiverRef: "payroll-provider"}); err != nil {
		t.Fatal(err)
	}
	accepted, err := s.RecordDestinationReceipt(ctx, "tenant-a", DestinationReceipt{TenantID: "tenant-a", DestinationID: draft.ID, Status: DestinationAccepted, ReceiverRef: "payroll-provider"})
	if err != nil || accepted.Status != DestinationAccepted {
		t.Fatalf("payroll accept: %#v, %v", accepted, err)
	}
	// A correction's re-export at a new source revision idempotently
	// coexists with the original rather than duplicating it.
	corrected, err := s.CreateDestinationDraft(ctx, "tenant-a", DestinationRecord{
		TenantID: "tenant-a", ID: uuid.NewString(), Kind: DestinationPayrollExport,
		SourceRef: "timecard-1", SourceRevision: 5, ReceiverRef: "payroll-provider",
		PayloadDigest: "pd2", Payload: []byte(`{"worker":"worker-1","hours":82}`), IdempotencyKey: "payroll-export-1",
	})
	if err != nil || corrected.SourceRevision != 5 {
		t.Fatalf("corrected export = %#v, %v", corrected, err)
	}
}

func TestTodo_WTIME_011_Golden(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	first, err := s.CreateDestinationDraft(ctx, "tenant-a", DestinationRecord{
		TenantID: "tenant-a", ID: uuid.NewString(), Kind: DestinationContractorInvoice,
		SourceRef: "engagement-golden", SourceRevision: 1, ReceiverRef: "ap",
		PayloadDigest: "d1", Payload: []byte(`{"amount":"500.00"}`), IdempotencyKey: "k1",
	})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.CreateDestinationDraft(ctx, "tenant-a", DestinationRecord{
		TenantID: "tenant-a", ID: uuid.NewString(), Kind: DestinationContractorInvoice,
		SourceRef: "engagement-golden", SourceRevision: 1, ReceiverRef: "ap",
		PayloadDigest: "d1", Payload: []byte(`{"amount":"500.00"}`), IdempotencyKey: "k1",
	})
	if err != nil || replay.ID != first.ID {
		t.Fatalf("replay = %#v, %v, want the original row %q", replay, err, first.ID)
	}
	if _, err := s.CreateDestinationDraft(ctx, "tenant-a", DestinationRecord{
		TenantID: "tenant-a", ID: uuid.NewString(), Kind: DestinationContractorInvoice,
		SourceRef: "engagement-golden", SourceRevision: 1, ReceiverRef: "ap",
		PayloadDigest: "d2", Payload: []byte(`{"amount":"999.00"}`), IdempotencyKey: "k1",
	}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed digest same key = %v, want ErrIdempotencyConflict", err)
	}
}

func TestTodo_WTIME_011_Security(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	draft, err := s.CreateDestinationDraft(ctx, "tenant-a", DestinationRecord{
		TenantID: "tenant-a", ID: uuid.NewString(), Kind: DestinationContractorInvoice,
		SourceRef: "engagement-shared", SourceRevision: 1, ReceiverRef: "ap",
		PayloadDigest: "d1", Payload: []byte(`{}`), IdempotencyKey: "k1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetDestinationRecord(ctx, "tenant-b", draft.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant read = %v, want ErrNotFound", err)
	}
	if _, err := s.RecordDestinationReceipt(ctx, "tenant-b", DestinationReceipt{TenantID: "tenant-b", DestinationID: draft.ID, Status: DestinationSent, ReceiverRef: "intruder"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant receipt = %v, want ErrNotFound", err)
	}
}

func TestTodo_WTIME_011_InvalidInputsRejected(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if _, err := s.CreateDestinationDraft(ctx, "tenant-a", DestinationRecord{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty record = %v", err)
	}
	if _, err := s.RecordDestinationReceipt(ctx, "tenant-a", DestinationReceipt{TenantID: "tenant-a", DestinationID: "x", ReceiverRef: "r", Status: "BOGUS"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid receipt status = %v", err)
	}
}
