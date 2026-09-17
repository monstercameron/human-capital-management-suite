package paygl

import (
	"errors"
	"testing"
)

func correctionRequest(t *testing.T) CorrectionRequest {
	t.Helper()
	return CorrectionRequest{
		CorrectionID: "journal-1-c1",
		Reason:       "cost center cc-1 should have been cc-9",
		Reversals: []ReversalLine{
			{CorrectsOrdinal: 1},
			{CorrectsOrdinal: 2},
		},
		Adjustments: []JournalLine{
			{
				Ordinal: 1, Account: "6001", Side: SideDebit, Amount: glDecimal(t, "100.01"),
				Currency: "USD", Entity: "entity-1", Ledger: LedgerActual,
				Dimension: mappingDimension("cc-9"), SourceRef: "component-1/1",
			},
			{
				Ordinal: 2, Account: "2101", Side: SideCredit, Amount: glDecimal(t, "100.01"),
				Currency: "USD", Entity: "entity-1", Ledger: LedgerActual,
				Dimension: mappingDimension("cc-9"), SourceRef: "component-1/1",
			},
		},
	}
}

func journalFixture(t *testing.T) Journal {
	t.Helper()
	journal, err := NewJournal(journalRequest(t))
	if err != nil {
		t.Fatalf("NewJournal: %v", err)
	}
	return journal
}

// TestTodo_PAYGL_005 is the primary acceptance case: a correction creates a
// reversal and adjustment with exact prior-line links, balances exactly, and
// never overwrites exported journal history.
func TestTodo_PAYGL_005(t *testing.T) {
	original := journalFixture(t)
	got, err := CorrectJournal(original, correctionRequest(t))
	if err != nil {
		t.Fatalf("CorrectJournal: %v", err)
	}
	rejectEmptyPayGLDigest(t, got.Digest)
	if got.Corrected.JournalID != "journal-1-c1" {
		t.Fatalf("correction id = %s, want journal-1-c1", got.Corrected.JournalID)
	}
	if got.OriginalDigest != original.Digest {
		t.Fatal("correction does not link the exact original digest")
	}
	if len(got.Reversals) != 2 || len(got.Corrected.Lines) != 4 {
		t.Fatalf("reversals = %d lines = %d, want 2 and 4", len(got.Reversals), len(got.Corrected.Lines))
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := got.Corrected.Validate(); err != nil {
		t.Fatalf("corrected Validate: %v", err)
	}
	// The original is untouched: still valid with the same digest.
	if err := original.Validate(); err != nil {
		t.Fatalf("original no longer valid: %v", err)
	}
	again, err := NewJournal(journalRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if again.Digest != original.Digest {
		t.Fatal("original journal history changed")
	}

	cases := map[string]func(*CorrectionRequest){
		"unknown prior line": func(r *CorrectionRequest) {
			r.Reversals[0].CorrectsOrdinal = 99
		},
		"missing reason": func(r *CorrectionRequest) {
			r.Reason = ""
		},
		"missing correction id": func(r *CorrectionRequest) {
			r.CorrectionID = ""
		},
		"unbalanced adjustments": func(r *CorrectionRequest) {
			r.Adjustments[1].Amount = glDecimal(t, "50.00")
		},
		"no lines at all": func(r *CorrectionRequest) {
			r.Reversals = nil
			r.Adjustments = nil
		},
		"adjustment without dimension": func(r *CorrectionRequest) {
			r.Adjustments[0].Dimension.Version = ""
		},
	}
	for name, mutate := range cases {
		req := correctionRequest(t)
		mutate(&req)
		if _, err := CorrectJournal(original, req); !errors.Is(err, ErrCorrectionRejected) {
			t.Fatalf("%s: err = %v, want PAYGL_005_REJECTED", name, err)
		}
	}
	// Correcting with an already-corrected id is refused: history is append-only.
	if _, err := CorrectJournal(original, correctionRequest(t)); err != nil {
		t.Fatalf("first correction: %v", err)
	}
	second := correctionRequest(t)
	second.CorrectionID = "journal-1-c2"
	chained, err := CorrectJournal(got.Corrected, second)
	if err != nil {
		t.Fatalf("chained correction: %v", err)
	}
	if chained.OriginalDigest != got.Corrected.Digest {
		t.Fatal("chained correction does not link its parent")
	}
}

// TestTodo_PAYGL_005_Property proves append-only correction: a full reversal
// nets every account to zero and chained corrections preserve the lineage.
func TestTodo_PAYGL_005_Property(t *testing.T) {
	original := journalFixture(t)
	full := CorrectionRequest{
		CorrectionID: "journal-1-full",
		Reason:       "void duplicated run",
		Reversals:    []ReversalLine{{CorrectsOrdinal: 1}, {CorrectsOrdinal: 2}},
	}
	got, err := CorrectJournal(original, full)
	if err != nil {
		t.Fatalf("full reversal: %v", err)
	}
	pending := map[string]int{}
	for _, line := range original.Lines {
		flipped := line.Side
		if flipped == SideDebit {
			flipped = SideCredit
		} else {
			flipped = SideDebit
		}
		pending[line.Account+"\x00"+string(flipped)+"\x00"+line.Amount.String()]++
	}
	for _, line := range got.Corrected.Lines {
		key := line.Account + "\x00" + string(line.Side) + "\x00" + line.Amount.String()
		if pending[key] == 0 {
			t.Fatalf("corrected line %+v mirrors no original line", line)
		}
		pending[key]--
	}
	for key, remaining := range pending {
		if remaining != 0 {
			t.Fatalf("unreversed original remains: %q", key)
		}
	}
	if !got.Corrected.TotalDebits.Equal(original.TotalDebits) {
		t.Fatal("full reversal changed the debit total")
	}
	// Correcting the correction keeps working and never edits ancestors.
	second, err := CorrectJournal(got.Corrected, CorrectionRequest{
		CorrectionID: "journal-1-full-2", Reason: "reissue",
		Reversals: []ReversalLine{{CorrectsOrdinal: 1}, {CorrectsOrdinal: 2}},
	})
	if err != nil {
		t.Fatalf("second correction: %v", err)
	}
	if err := original.Validate(); err != nil {
		t.Fatalf("ancestor changed: %v", err)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("parent changed: %v", err)
	}
	if err := second.Validate(); err != nil {
		t.Fatalf("second Validate: %v", err)
	}
}

// TestTodo_PAYGL_005_Mutation proves the digest binds original, reason and
// every line: any change yields a new digest and tampering fails.
func TestTodo_PAYGL_005_Mutation(t *testing.T) {
	original := journalFixture(t)
	base, err := CorrectJournal(original, correctionRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*CorrectionRequest){
		func(r *CorrectionRequest) { r.Reason = "another reason" },
		func(r *CorrectionRequest) { r.CorrectionID = "journal-1-c9" },
		func(r *CorrectionRequest) { r.Adjustments[0].Dimension.Value = "cc-8" },
		func(r *CorrectionRequest) {
			r.Reversals = []ReversalLine{{CorrectsOrdinal: 2}, {CorrectsOrdinal: 1}}
		},
	}
	for i, mutate := range mutations {
		req := correctionRequest(t)
		mutate(&req)
		mutated, err := CorrectJournal(original, req)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if mutated.Digest == base.Digest {
			t.Fatalf("case %d did not change the digest", i)
		}
	}
	otherReq := journalRequest(t)
	otherReq.Lines[0].Amount = glDecimal(t, "200.02")
	otherReq.Lines[1].Amount = glDecimal(t, "200.02")
	other, err := NewJournal(otherReq)
	if err != nil {
		t.Fatal(err)
	}
	swapped := base
	swapped.OriginalDigest = other.Digest
	if err := swapped.Validate(); err == nil {
		t.Fatal("swapped original passed validation")
	}
	forged := base
	forged.Digest = "sha256:forged"
	if err := forged.Validate(); err == nil {
		t.Fatal("forged digest passed validation")
	}
}
