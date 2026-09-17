package paygl

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/labor"
)

func journalLine(t *testing.T, ordinal int, side DebitCredit, account, amount string) JournalLine {
	t.Helper()
	return JournalLine{
		Ordinal:   ordinal,
		Account:   account,
		Side:      side,
		Amount:    glDecimal(t, amount),
		Currency:  "USD",
		Entity:    "entity-1",
		Ledger:    LedgerActual,
		Dimension: labor.Dimension{Kind: labor.DimensionCostCenter, Value: "cc-1", Version: "v1"},
		SourceRef: "component-1/1",
	}
}

func journalRequest(t *testing.T) JournalRequest {
	t.Helper()
	return JournalRequest{
		JournalID:         "journal-1",
		SourceRunID:       "run-gl",
		SourceRunRevision: 3,
		SourceRunDigest:   "sha256:run-gl",
		Lines: []JournalLine{
			journalLine(t, 1, SideDebit, "6001", "100.01"),
			journalLine(t, 2, SideCredit, "2101", "100.01"),
		},
	}
}

// TestTodo_PAYGL_004 is the primary acceptance case: debits equal credits by
// currency, entity and ledger, every line binds a governed dimension, and
// the journal is immutable, content-addressed and source-run linked.
func TestTodo_PAYGL_004(t *testing.T) {

	got, err := NewJournal(journalRequest(t))
	if err != nil {
		t.Fatalf("NewJournal: %v", err)
	}
	rejectEmptyPayGLDigest(t, got.Digest)
	if got.TotalDebits.String() != "100.01" || got.TotalCredits.String() != "100.01" {
		t.Fatalf("totals = %s/%s, want 100.01/100.01", got.TotalDebits, got.TotalCredits)
	}
	if got.SourceRunID != "run-gl" || got.SourceRunRevision != 3 || got.SourceRunDigest != "sha256:run-gl" {
		t.Fatalf("source link = %+v, want run-gl/3", got)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	cases := map[string]func(*JournalRequest){
		"unbalanced": func(r *JournalRequest) {
			r.Lines[1].Amount = glDecimal(t, "100.00")
		},
		"missing dimension": func(r *JournalRequest) {
			r.Lines[0].Dimension = labor.Dimension{}
		},
		"dimension version missing": func(r *JournalRequest) {
			r.Lines[0].Dimension.Version = ""
		},
		"mixed currency": func(r *JournalRequest) {
			r.Lines[1].Currency = "EUR"
			r.Lines[1].Amount = glDecimal(t, "100.01")
		},
		"mixed entity": func(r *JournalRequest) {
			r.Lines[1].Entity = "entity-2"
		},
		"empty account": func(r *JournalRequest) {
			r.Lines[0].Account = ""
		},
		"bad side": func(r *JournalRequest) {
			r.Lines[0].Side = "BOTH"
		},
		"missing source link": func(r *JournalRequest) {
			r.SourceRunDigest = ""
		},
		"duplicate ordinal": func(r *JournalRequest) {
			r.Lines[1].Ordinal = 1
		},
	}
	for name, mutate := range cases {
		req := journalRequest(t)
		mutate(&req)
		if _, err := NewJournal(req); !errors.Is(err, ErrJournalRejected) {
			t.Fatalf("%s: err = %v, want PAYGL_004_REJECTED", name, err)
		}
	}
}

// TestTodo_PAYGL_004_Property proves balance per group and determinism over
// generated multi-line journals.
func TestTodo_PAYGL_004_Property(t *testing.T) {

	rng := rand.New(rand.NewSource(20260917))
	for i := 0; i < 48; i++ {
		lines := []JournalLine{}
		pairs := 1 + rng.Intn(4)
		ordinal := 0
		for p := 0; p < pairs; p++ {
			cents := 1 + rng.Intn(99999)
			amount := itoaPayGL(cents)
			ordinal++
			lines = append(lines, journalLine(t, ordinal, SideDebit, "600"+itoaPayGLWhole(p), amount))
			ordinal++
			lines = append(lines, journalLine(t, ordinal, SideCredit, "210"+itoaPayGLWhole(p), amount))
		}
		for s := range lines {
			other := rng.Intn(len(lines))
			lines[s], lines[other] = lines[other], lines[s]
		}
		req := journalRequest(t)
		req.JournalID = "journal-prop"
		req.Lines = lines
		got, err := NewJournal(req)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if !got.TotalDebits.Equal(got.TotalCredits) {
			t.Fatalf("case %d: debits %s != credits %s", i, got.TotalDebits, got.TotalCredits)
		}
		again, err := NewJournal(req)
		if err != nil {
			t.Fatalf("case %d repeat: %v", i, err)
		}
		if again.Digest != got.Digest {
			t.Fatalf("case %d: digest not deterministic", i)
		}
	}
}

// TestTodo_PAYGL_004_Mutation proves the digest binds every line: any change
// yields a new digest and tampering fails validation.
func TestTodo_PAYGL_004_Mutation(t *testing.T) {

	base, err := NewJournal(journalRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	mutations := []func(*JournalRequest){
		func(r *JournalRequest) {
			r.Lines[0].Amount = glDecimal(t, "100.02")
			r.Lines[1].Amount = glDecimal(t, "100.02")
		},
		func(r *JournalRequest) { r.Lines[0].Account = "6002"; r.Lines[0].SourceRef = "component-1/1" },
		func(r *JournalRequest) { r.Lines[0].Dimension.Value = "cc-2" },
		func(r *JournalRequest) { r.SourceRunRevision = 4 },
		func(r *JournalRequest) { r.Lines[0].Side = SideCredit; r.Lines[1].Side = SideDebit },
	}
	for i, mutate := range mutations {
		req := journalRequest(t)
		mutate(&req)
		mutated, err := NewJournal(req)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if mutated.Digest == base.Digest {
			t.Fatalf("case %d did not change the digest", i)
		}
	}
	tampered := base
	tampered.Lines[0].Amount = glDecimal(t, "0.01")
	if err := tampered.Validate(); err == nil {
		t.Fatal("tampered line passed validation")
	}
	undimensioned := base
	undimensioned.Lines[1].Dimension = labor.Dimension{}
	if err := undimensioned.Validate(); err == nil {
		t.Fatal("dimension stripped by hand passed validation")
	}
	forged := base
	forged.Digest = "sha256:forged"
	if err := forged.Validate(); err == nil {
		t.Fatal("forged digest passed validation")
	}
}
