package availability

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/balance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func ordinaryAbsenceDefinition() balance.AccumulatorDefinition {
	return balance.AccumulatorDefinition{
		ID: "pto-hours", Version: "v1", Name: "PTO hours",
		Unit: "HOUR", Currency: "HOUR", Subject: "worker", Period: balance.PeriodCalendarYear,
		Dimensions: []balance.BalanceDimension{{Name: "worker_id", ValueType: "worker_id", Required: true}},
		EntryTypes: []string{"ABSENCE_DEBIT", "ABSENCE_RESTORE"},
		Authority:  balance.Authority{SourceID: "absence", Version: "v1"},
	}
}

func ordinaryAbsenceResult(t *testing.T) AbsenceWindowResult {
	t.Helper()
	req := versionedAbsenceRequest(t)
	req.ExistingAbsences = nil
	result, err := SimulateAbsenceWindow(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Conflicts) != 0 || result.Hours <= 0 {
		t.Fatalf("fixture result = %+v, want clean hours", result)
	}
	return result
}

func ordinaryWorker(t *testing.T) values.EntityRef {
	t.Helper()
	return values.EntityRef{Tenant: values.TenantId("acme"), Kind: KindWorker, Id: "00000000-0000-4000-8000-000000000003"}
}

// TestTodo_WF_CAP_004 is the PRIMARY test: absence.commit posts the absence
// and the balance debit, absence.cancel restores it, and team coverage
// reports overlap at the caller's permitted granularity.
func TestTodo_WF_CAP_004(t *testing.T) {
	ledger := NewOrdinaryLedger()
	store := balance.NewEntryStoreWithClock(func() time.Time { return time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC) })
	def := ordinaryAbsenceDefinition()
	result := ordinaryAbsenceResult(t)
	worker := ordinaryWorker(t)

	committed, err := CommitOrdinaryAbsence(ledger, store, def, OrdinaryCommitRequest{
		Worker: worker, Account: "pto:worker-3", Result: result, Key: "absence-1",
		At: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CommitOrdinaryAbsence: %v", err)
	}
	if committed.State != OrdinaryCommitted || committed.DebitDigest == "" {
		t.Fatalf("committed = %+v, want COMMITTED with a debit digest", committed)
	}
	entries := store.Entries("pto:worker-3")
	if len(entries) != 1 || entries[0].Kind != balance.Debit {
		t.Fatalf("posted entries = %+v, want one DEBIT", entries)
	}

	replay, err := CommitOrdinaryAbsence(ledger, store, def, OrdinaryCommitRequest{
		Worker: worker, Account: "pto:worker-3", Result: result, Key: "absence-1",
		At: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil || replay.ID != committed.ID || len(store.Entries("pto:worker-3")) != 1 {
		t.Fatalf("idempotent recommit = %+v, %v; want the original receipt and no second debit", replay, err)
	}

	overlapReq := versionedAbsenceRequest(t)
	conflicted, err := SimulateAbsenceWindow(overlapReq)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicted.Conflicts) == 0 || !conflicted.Verify() {
		t.Fatalf("fixture conflicted result = %+v, want verified conflicts", conflicted)
	}
	if _, err := CommitOrdinaryAbsence(ledger, store, def, OrdinaryCommitRequest{
		Worker: worker, Account: "pto:worker-3", Result: conflicted, Key: "absence-2",
		At: time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
	}); !errors.Is(err, ErrOrdinaryAbsenceConflicted) {
		t.Fatalf("conflicted commit: got %v, want ErrOrdinaryAbsenceConflicted", err)
	}

	counts, err := TeamCoverage(ledger.Committed(), committed.Dates, CoverageCounts)
	if err != nil {
		t.Fatalf("TeamCoverage counts: %v", err)
	}
	if len(counts) == 0 || counts[0].AbsentCount != 1 || len(counts[0].Workers) != 0 {
		t.Fatalf("counts coverage = %+v, want counts with no identities", counts)
	}
	identified, err := TeamCoverage(ledger.Committed(), committed.Dates, CoverageIdentified)
	if err != nil {
		t.Fatalf("TeamCoverage identified: %v", err)
	}
	if len(identified) == 0 || len(identified[0].Workers) != 1 {
		t.Fatalf("identified coverage = %+v, want the absent worker named", identified)
	}
	if _, err := TeamCoverage(ledger.Committed(), committed.Dates, CoverageGranularity("SECRET")); !errors.Is(err, ErrOrdinaryCoverageDenied) {
		t.Fatalf("unknown granularity: got %v, want ErrOrdinaryCoverageDenied", err)
	}

	cancelled, err := CancelOrdinaryAbsence(ledger, store, def, committed.ID, "cancel-1", time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("CancelOrdinaryAbsence: %v", err)
	}
	if cancelled.State != OrdinaryCancelled {
		t.Fatalf("cancelled = %+v, want CANCELLED", cancelled)
	}
	posted := store.Entries("pto:worker-3")
	if len(posted) != 2 || posted[1].Kind != balance.Credit || !posted[1].Amount.Equal(posted[0].Amount) {
		t.Fatalf("posted entries = %+v, want DEBIT then an equal CREDIT", posted)
	}
	if _, err := CancelOrdinaryAbsence(ledger, store, def, committed.ID, "cancel-1", time.Date(2026, 4, 2, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("idempotent re-cancel: %v", err)
	}
	if len(store.Entries("pto:worker-3")) != 2 {
		t.Fatal("re-cancel posted a second credit")
	}
}

// TestTodo_WF_CAP_004_Property proves commit then cancel restores the
// balance exactly: across randomized absences the signed posted amounts
// always net to zero and every absence ends CANCELLED.
func TestTodo_WF_CAP_004_Property(t *testing.T) {
	ledger := NewOrdinaryLedger()
	clock := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	store := balance.NewEntryStoreWithClock(func() time.Time { return clock })
	def := ordinaryAbsenceDefinition()
	worker := ordinaryWorker(t)
	for trial := 0; trial < 50; trial++ {
		account := "pto:worker-3"
		result := ordinaryAbsenceResult(t)
		key := "absence-prop-" + time.Duration(trial).String()
		committed, err := CommitOrdinaryAbsence(ledger, store, def, OrdinaryCommitRequest{
			Worker: worker, Account: account, Result: result, Key: key, At: clock,
		})
		if err != nil {
			t.Fatalf("trial %d: commit: %v", trial, err)
		}
		clock = clock.Add(time.Hour)
		if _, err := CancelOrdinaryAbsence(ledger, store, def, committed.ID, key+"/cancel", clock); err != nil {
			t.Fatalf("trial %d: cancel: %v", trial, err)
		}
		clock = clock.Add(time.Hour)
	}
	entries := store.Entries("pto:worker-3")
	if len(entries) != 100 {
		t.Fatalf("posted entries = %d, want 100 (50 debits + 50 credits)", len(entries))
	}
	zero, err := entries[0].Amount.Sub(entries[0].Amount)
	if err != nil {
		t.Fatal(err)
	}
	net := zero
	for _, entry := range entries {
		signed := entry.Amount
		if entry.Kind == balance.Debit {
			signed, err = entry.Amount.Neg()
			if err != nil {
				t.Fatal(err)
			}
		}
		net, err = net.Add(signed)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !net.Equal(zero) {
		t.Fatalf("net posted amount = %s, want exactly zero", net)
	}
	for _, absence := range ledger.Committed() {
		t.Fatalf("absence %s still committed after cancel: %+v", absence.ID, absence)
	}
}
