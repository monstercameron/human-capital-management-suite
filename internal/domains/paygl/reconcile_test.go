package paygl

import (
	"errors"
	"testing"
)

func erpObservation(journal Journal) ERPObservation {
	lines := make([]ERPLine, len(journal.Lines))
	for i, line := range journal.Lines {
		lines[i] = ERPLine{
			JournalOrdinal: line.Ordinal,
			ExternalID:     "erp-post-" + itoaPayGLWhole(line.Ordinal),
			Amount:         line.Amount,
			Currency:       line.Currency,
			Dimensions:     []string{line.Dimension.Value},
			State:          ERPLineAccepted,
		}
	}
	return ERPObservation{
		Source:        "erp-adapter/prod",
		BatchID:       "erp-batch-7",
		JournalDigest: journal.Digest,
		Lines:         lines,
		Total:         journal.TotalDebits,
		Currency:      journal.Currency(),
	}
}

// TestTodo_PAYGL_006 is the primary acceptance case: provider-accepted alone
// never settles; journal, lines, totals, dimensions and external IDs are all
// compared, and every reject, partial or unknown routes to repair.
func TestTodo_PAYGL_006(t *testing.T) {
	journal := journalFixture(t)
	rec, err := ReconcilePosting(journal, erpObservation(journal))
	if err != nil {
		t.Fatalf("ReconcilePosting: %v", err)
	}
	rejectEmptyPayGLDigest(t, rec.Digest)
	if rec.Aggregate != PostingSettled {
		t.Fatalf("aggregate = %s, want SETTLED", rec.Aggregate)
	}
	if len(rec.Repairs) != 0 {
		t.Fatalf("repairs = %v, want none", rec.Repairs)
	}
	if err := rec.Validate(journal); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// Accepted with the wrong amount never settles.
	wrongAmount := erpObservation(journal)
	wrongAmount.Lines[0].Amount = glDecimal(t, "99.99")
	rec, err = ReconcilePosting(journal, wrongAmount)
	if err != nil {
		t.Fatalf("wrong amount: %v", err)
	}
	if rec.Aggregate != PostingRepairRequired {
		t.Fatalf("wrong amount aggregate = %s, want REPAIR_REQUIRED", rec.Aggregate)
	}
	if rec.Lines[0].Status != PostingMismatch || len(rec.Repairs) == 0 {
		t.Fatalf("wrong amount not routed to repair: %+v", rec)
	}

	// Accepted with a lost dimension never settles.
	lostDim := erpObservation(journal)
	lostDim.Lines[1].Dimensions = []string{"cc-other"}
	rec, err = ReconcilePosting(journal, lostDim)
	if err != nil {
		t.Fatalf("lost dimension: %v", err)
	}
	if rec.Aggregate != PostingRepairRequired || rec.Lines[1].Status != PostingMismatch {
		t.Fatalf("lost dimension not routed to repair: %+v", rec)
	}

	// Rejected and partial lines route to repair.
	rejected := erpObservation(journal)
	rejected.Lines[0].State = ERPLineRejected
	rec, err = ReconcilePosting(journal, rejected)
	if err != nil {
		t.Fatalf("rejected: %v", err)
	}
	if rec.Aggregate != PostingRepairRequired || rec.Lines[0].Status != PostingMismatch {
		t.Fatalf("rejected not routed to repair: %+v", rec)
	}
	partial := erpObservation(journal)
	partial.Lines[1].State = ERPLinePartial
	rec, err = ReconcilePosting(journal, partial)
	if err != nil {
		t.Fatalf("partial: %v", err)
	}
	if rec.Aggregate != PostingRepairRequired {
		t.Fatalf("partial aggregate = %s, want REPAIR_REQUIRED", rec.Aggregate)
	}

	// Unknown lines stay unknown and need re-observation, not repair blame.
	unknown := erpObservation(journal)
	unknown.Lines[0].State = ERPLineUnknown
	rec, err = ReconcilePosting(journal, unknown)
	if err != nil {
		t.Fatalf("unknown: %v", err)
	}
	if rec.Lines[0].Status != PostingUnknown {
		t.Fatalf("unknown line status = %s, want UNKNOWN", rec.Lines[0].Status)
	}
	if rec.Aggregate != PostingUnknownAggregate {
		t.Fatalf("unknown aggregate = %s, want UNKNOWN", rec.Aggregate)
	}

	// A short batch total creates a totals-scoped repair.
	short := erpObservation(journal)
	short.Total = glDecimal(t, "50.00")
	rec, err = ReconcilePosting(journal, short)
	if err != nil {
		t.Fatalf("short total: %v", err)
	}
	if rec.Aggregate != PostingRepairRequired {
		t.Fatalf("short aggregate = %s, want REPAIR_REQUIRED", rec.Aggregate)
	}
	found := false
	for _, repair := range rec.Repairs {
		if repair.Scope == PostingRepairTotals {
			found = true
		}
	}
	if !found {
		t.Fatalf("no totals repair: %+v", rec.Repairs)
	}

	// Malformed observations are refused, not reconciled.
	if _, err := ReconcilePosting(journal, ERPObservation{}); !errors.Is(err, ErrPostingReconciliationRejected) {
		t.Fatalf("empty observation: err = %v, want PAYGL_006_REJECTED", err)
	}
	foreign := erpObservation(journal)
	foreign.JournalDigest = "sha256:another-journal"
	if _, err := ReconcilePosting(journal, foreign); !errors.Is(err, ErrPostingReconciliationRejected) {
		t.Fatalf("foreign digest: err = %v, want PAYGL_006_REJECTED", err)
	}
}

// TestTodo_PAYGL_006_Property proves settlement happens exactly under full
// agreement: any single deviation removes SETTLED.
func TestTodo_PAYGL_006_Property(t *testing.T) {
	journal := journalFixture(t)
	base, err := ReconcilePosting(journal, erpObservation(journal))
	if err != nil {
		t.Fatal(err)
	}
	if base.Aggregate != PostingSettled {
		t.Fatal("fixture does not settle")
	}
	deviations := []func(*ERPObservation){
		func(o *ERPObservation) { o.Lines[0].Amount = glDecimal(t, "0.01") },
		func(o *ERPObservation) { o.Lines[1].ExternalID = "" },
		func(o *ERPObservation) { o.Lines[0].Dimensions = nil },
		func(o *ERPObservation) { o.Lines[1].State = ERPLineRejected },
		func(o *ERPObservation) { o.Lines[0].State = ERPLinePartial },
		func(o *ERPObservation) { o.Lines[1].State = ERPLineUnknown },
		func(o *ERPObservation) { o.Total = glDecimal(t, "0.01") },
		func(o *ERPObservation) { o.Currency = "EUR" },
		func(o *ERPObservation) { o.Lines = o.Lines[:1] },
	}
	for i, deviate := range deviations {
		obs := erpObservation(journal)
		deviate(&obs)
		rec, err := ReconcilePosting(journal, obs)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if rec.Aggregate == PostingSettled {
			t.Fatalf("case %d settled despite deviation", i)
		}
		if len(rec.Repairs) == 0 {
			t.Fatalf("case %d has no repair", i)
		}
	}
}

// TestTodo_PAYGL_006_Integration proves the store port returns the same
// typed result as the direct call, and a missing batch is a typed error.
func TestTodo_PAYGL_006_Integration(t *testing.T) {
	journal := journalFixture(t)
	store := NewMemoryERPStore()
	want, err := ReconcilePosting(journal, erpObservation(journal))
	if err != nil {
		t.Fatal(err)
	}
	store.PutObservation(erpObservation(journal))
	obs, err := store.ReadObservation(journal.Digest)
	if err != nil {
		t.Fatalf("ReadObservation: %v", err)
	}
	got, err := ReconcilePosting(journal, obs)
	if err != nil {
		t.Fatalf("ReconcilePosting through store: %v", err)
	}
	if got.Digest != want.Digest || got.Aggregate != want.Aggregate || len(got.Lines) != len(want.Lines) {
		t.Fatalf("store result %+v differs from direct %+v", got, want)
	}
	if _, err := store.ReadObservation("sha256:missing"); !errors.Is(err, ErrPostingReconciliationRejected) {
		t.Fatalf("missing batch: err = %v, want PAYGL_006_REJECTED", err)
	}
	if err := store.PutObservation(ERPObservation{}); !errors.Is(err, ErrPostingReconciliationRejected) {
		t.Fatalf("malformed put: err = %v, want PAYGL_006_REJECTED", err)
	}
}

// TestTodo_PAYGL_006_Mutation proves the digest binds journal, batch and
// every line outcome: any change yields a new digest and tampering fails.
func TestTodo_PAYGL_006_Mutation(t *testing.T) {
	journal := journalFixture(t)
	base, err := ReconcilePosting(journal, erpObservation(journal))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*ERPObservation){
		func(o *ERPObservation) { o.BatchID = "erp-batch-8" },
		func(o *ERPObservation) { o.Lines[0].ExternalID = "erp-post-x" },
		func(o *ERPObservation) { o.Lines[1].State = ERPLineUnknown },
	}
	for i, mutate := range mutations {
		obs := erpObservation(journal)
		mutate(&obs)
		mutated, err := ReconcilePosting(journal, obs)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if mutated.Digest == base.Digest {
			t.Fatalf("case %d did not change the digest", i)
		}
	}
	tampered := base
	tampered.Aggregate = PostingSettled
	tampered.Lines[0].Status = PostingMismatch
	if err := tampered.Validate(journal); err == nil {
		t.Fatal("tampered outcome passed validation")
	}
	forged := base
	forged.Digest = "sha256:forged"
	if err := forged.Validate(journal); err == nil {
		t.Fatal("forged digest passed validation")
	}
}
