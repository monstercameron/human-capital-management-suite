package evidence

import (
	"context"
	"errors"
	"testing"

	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

// TestRequiredFieldsFailClosed proves every structurally-required field on
// both methods is refused as INVALID_ARGUMENT when empty -- a zero value
// never means "unset, so permissive" for these two governed methods.
func TestRequiredFieldsFailClosed(t *testing.T) {
	h := newHarness(t)

	invalidArg := func(t *testing.T, err error) {
		t.Helper()
		var owned *envelope.Error
		if !errors.As(err, &owned) || owned.Code() != envelope.CodeInvalidArgument {
			t.Fatalf("expected INVALID_ARGUMENT, got: %v", err)
		}
	}

	t.Run("GetExecutionReceipt_EmptyReceiptID", func(t *testing.T) {
		req := &evidencev1.GetExecutionReceiptRequest{}
		ctx := admittedContext(t, testSubject, []string{testPurpose}, req, GetExecutionReceiptProcedure)
		_, err := h.server.GetExecutionReceipt(ctx, req)
		invalidArg(t, err)
	})

	t.Run("ExportIntentEvidence_EmptyIdempotencyKey", func(t *testing.T) {
		req := &evidencev1.ExportIntentEvidenceRequest{IntentId: "intent-x", Purpose: testPurpose}
		ctx := admittedContext(t, testSubject, []string{testPurpose}, req, ExportIntentEvidenceProcedure)
		_, err := h.server.ExportIntentEvidence(ctx, req)
		invalidArg(t, err)
	})

	t.Run("ExportIntentEvidence_EmptyIntentID", func(t *testing.T) {
		req := &evidencev1.ExportIntentEvidenceRequest{IdempotencyKey: "idem-x", Purpose: testPurpose}
		ctx := admittedContext(t, testSubject, []string{testPurpose}, req, ExportIntentEvidenceProcedure)
		_, err := h.server.ExportIntentEvidence(ctx, req)
		invalidArg(t, err)
	})

	t.Run("ExportIntentEvidence_EmptyPurpose", func(t *testing.T) {
		req := &evidencev1.ExportIntentEvidenceRequest{IdempotencyKey: "idem-x", IntentId: "intent-x"}
		ctx := admittedContext(t, testSubject, []string{testPurpose}, req, ExportIntentEvidenceProcedure)
		_, err := h.server.ExportIntentEvidence(ctx, req)
		invalidArg(t, err)
	})

	if creates, _ := h.ops.Writes(); creates != 0 {
		t.Fatalf("a structurally invalid request must never create an operation, created %d", creates)
	}
}

// TestIdempotencyKeyReusedWithDifferentPayloadIsAborted proves the
// PayloadConflict branch of idempotencyError: the same idempotency key bound
// to two different logical requests (different intent_id) is refused rather
// than silently resolving to either one.
func TestIdempotencyKeyReusedWithDifferentPayloadIsAborted(t *testing.T) {
	h := newHarness(t)
	h.dispatch.sync = true
	h.lineage.Seed(testTenant, "intent-a", fullLineage(testTenant, "intent-a", "", ""))
	h.lineage.Seed(testTenant, "intent-b", fullLineage(testTenant, "intent-b", "", ""))

	first := &evidencev1.ExportIntentEvidenceRequest{IdempotencyKey: "shared-key", IntentId: "intent-a", Purpose: testPurpose}
	ctx1 := admittedContext(t, testSubject, []string{testPurpose}, first, ExportIntentEvidenceProcedure)
	if _, err := h.server.ExportIntentEvidence(ctx1, first); err != nil {
		t.Fatalf("first export: %v", err)
	}

	second := &evidencev1.ExportIntentEvidenceRequest{IdempotencyKey: "shared-key", IntentId: "intent-b", Purpose: testPurpose}
	ctx2 := admittedContext(t, testSubject, []string{testPurpose}, second, ExportIntentEvidenceProcedure)
	_, err := h.server.ExportIntentEvidence(ctx2, second)
	var owned *envelope.Error
	if !errors.As(err, &owned) || owned.Code() != envelope.CodeAborted {
		t.Fatalf("expected ABORTED for a reused idempotency key with a different payload, got: %v", err)
	}
	if creates, _ := h.ops.Writes(); creates != 1 {
		t.Fatalf("a rejected payload conflict must not create a second operation, created %d", creates)
	}
}

// TestGetExecutionReceiptUnavailableOnStoreFailure proves a store failure
// that is not the not-found sentinel is projected as UNAVAILABLE rather than
// a disclosing or misleading code.
func TestGetExecutionReceiptUnavailableOnStoreFailure(t *testing.T) {
	h := newHarness(t)
	h.server.deps.Receipts = brokenReceiptStore{}
	req := &evidencev1.GetExecutionReceiptRequest{ReceiptId: "receipt-x"}
	ctx := admittedContext(t, testSubject, []string{testPurpose}, req, GetExecutionReceiptProcedure)
	_, err := h.server.GetExecutionReceipt(ctx, req)
	var owned *envelope.Error
	if !errors.As(err, &owned) || owned.Code() != envelope.CodeUnavailable {
		t.Fatalf("expected UNAVAILABLE for a broken store, got: %v", err)
	}
}

type brokenReceiptStore struct{}

func (brokenReceiptStore) Get(context.Context, string, string) (Receipt, error) {
	return Receipt{}, errors.New("evidence: simulated store outage")
}

// TestGoDispatcherRunsOffTheCallingGoroutine proves the production
// [Dispatcher] actually hands work to a separate goroutine rather than
// running it inline, which is the concrete mechanism behind "the request
// path never builds the archive synchronously".
func TestGoDispatcherRunsOffTheCallingGoroutine(t *testing.T) {
	release := make(chan struct{})
	done := make(chan struct{})

	(&GoDispatcher{}).Dispatch(context.Background(), exportJob{OperationID: "op-dispatch-test"}, func(ctx context.Context, job exportJob) {
		<-release // blocks until this test explicitly releases it
		if job.OperationID != "op-dispatch-test" {
			t.Errorf("dispatched job lost its identity: %+v", job)
		}
		close(done)
	})

	// Dispatch must have returned already -- it is a goroutine boundary, not
	// a function call the request path waits on -- so the job's own block on
	// release proves nothing here blocked the caller.
	select {
	case <-done:
		t.Fatal("job ran to completion before being released; Dispatch did not hand it to a separate goroutine")
	default:
	}
	close(release)
	<-done
}
