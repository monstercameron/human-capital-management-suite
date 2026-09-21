package balance

import (
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func transferInstant(t *testing.T, text string) values.Instant {
	t.Helper()
	v, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatal(err)
	}
	return values.NewInstant(v)
}

// transferFixture posts a 10-hour GRANT move to worker-1 and builds the
// matching reissue posting for worker-2.
func transferFixture(t *testing.T) (AccumulatorDefinition, BalanceEntry, []BalanceEntry, BalanceEntry) {
	t.Helper()
	d := validDefinition()
	at := transferInstant(t, "2026-06-01T00:00:00Z")
	recorded := transferInstant(t, "2026-06-02T00:00:00Z")
	original := validEntry()
	original.Kind = Credit
	original.EntryType = "GRANT"
	original.EffectiveAt, original.RecordedAt, original.AuthorizedAt = at, recorded, recorded
	original.Amount = values.MustDecimal("10.00", 2, values.RoundingExactRequired)
	reissue := original.copy()
	reissue.AccountID = "worker-2"
	reissue.Dimensions = map[string]string{"worker_id": "worker-2", "program": "pto"}
	reissue.SourceTransactionID = "transfer-1/reissue"
	reissue.IdempotencyKey = "transfer-1/reissue"
	return d, original, []BalanceEntry{original}, reissue
}

func transferRequest(t *testing.T, d AccumulatorDefinition, original BalanceEntry, ledger []BalanceEntry, reissue BalanceEntry) TransferRequest {
	t.Helper()
	zero := values.MustDecimal("0.00", 2, values.RoundingExactRequired)
	return TransferRequest{
		Definition:     d,
		Original:       original,
		FromLedger:     ledger,
		FromOpening:    zero,
		Reissue:        reissue,
		ToOpening:      zero,
		RecordedAt:     transferInstant(t, "2026-06-03T00:00:00Z"),
		AuthorizedAt:   transferInstant(t, "2026-06-03T00:00:00Z"),
		IdempotencyKey: "transfer-1",
	}
}

// TestTodo_REV_039_03 proves a posted move transfers between accumulators:
// the reversal leg negates the original through the correction path, the
// reissue leg mirrors the move into the destination, and both legs settle
// atomically with deterministic receipts.
func TestTodo_REV_039_03(t *testing.T) {
	d, original, ledger, reissue := transferFixture(t)
	first, err := Transfer(transferRequest(t, d, original, ledger, reissue))
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	if first.Reversal.Kind != Debit || first.Reversal.Amount.String() != "10.00" {
		t.Fatalf("reversal = %+v, want a Debit contra-entry for 10.00", first.Reversal)
	}
	if first.Reversal.SupersedesDigest != original.Digest() || first.Reversal.EntryType != AdjustmentEntryType {
		t.Fatalf("reversal = %+v, want a correction-path entry superseding the original", first.Reversal)
	}
	if first.Reissue.AccountID != "worker-2" || first.Reissue.Kind != Credit || first.Reissue.Amount.String() != "10.00" {
		t.Fatalf("reissue = %+v, want the mirrored move in worker-2", first.Reissue)
	}
	if first.FromBalance.Ending.String() != "0.00" || first.ToBalance.Ending.String() != "10.00" {
		t.Fatalf("balances = %s/%s, want 0.00/10.00", first.FromBalance.Ending, first.ToBalance.Ending)
	}
	if first.FromReceipt.Entries != 1 || first.ToReceipt.Entries != 1 || first.Digest == "" {
		t.Fatalf("result = %+v, want one entry per receipt and a digest", first)
	}
	second, err := Transfer(transferRequest(t, d, original, ledger, reissue))
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatal("transfer is not deterministic")
	}
}

// TestTodo_REV_039_03_Property proves transfer guards: an unknown original,
// an already-reversed move, and a value-changing reissue are all refused.
func TestTodo_REV_039_03_Property(t *testing.T) {
	d, original, ledger, reissue := transferFixture(t)
	ghost := original.copy()
	ghost.SourceTransactionID = "ghost"
	ghost.IdempotencyKey = "ghost"
	if _, err := Transfer(transferRequest(t, d, ghost, ledger, reissue)); err == nil {
		t.Fatal("Transfer accepted an original that is not a ledger member")
	}
	first, err := Transfer(transferRequest(t, d, original, ledger, reissue))
	if err != nil {
		t.Fatalf("Transfer: %v", err)
	}
	ledger = append(append([]BalanceEntry(nil), ledger...), first.Reversal)
	if _, err := Transfer(transferRequest(t, d, original, ledger, reissue)); err == nil {
		t.Fatal("Transfer reversed an already-reversed move")
	}
	changed := reissue.copy()
	changed.Amount = values.MustDecimal("9.00", 2, values.RoundingExactRequired)
	if _, err := Transfer(transferRequest(t, d, original, []BalanceEntry{original}, changed)); err == nil {
		t.Fatal("Transfer accepted a reissue that changes the moved value")
	}
}

// TestTodo_REV_039_03_Race proves concurrent transfers over disjoint moves
// settle independently without sharing state.
func TestTodo_REV_039_03_Race(t *testing.T) {
	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d, original, ledger, reissue := transferFixture(t)
			key := "transfer-race"
			reissue.SourceTransactionID = key
			reissue.IdempotencyKey = key
			req := transferRequest(t, d, original, ledger, reissue)
			req.IdempotencyKey = key
			_, errs[i] = Transfer(req)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
}
