package application_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/labor"
	paygldomain "github.com/monstercameron/human-capital-management-suite/internal/domains/paygl"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func servedPayGLJournal(t *testing.T) paygldomain.Journal {
	t.Helper()
	amount := values.MustDecimal("100.00", 2, values.RoundingExactRequired)
	dimension := labor.Dimension{Kind: labor.DimensionCostCenter, Value: "cc-1", Version: "v1"}
	journal, err := paygldomain.NewJournal(paygldomain.JournalRequest{
		JournalID: "journal-served-1", SourceRunID: "payroll-run-1", SourceRunRevision: 1,
		SourceRunDigest: "sha256:payroll-run-1",
		Lines: []paygldomain.JournalLine{
			{Ordinal: 1, Account: "6001", Side: paygldomain.SideDebit, Amount: amount, Currency: "USD", Entity: "entity-1", Ledger: paygldomain.LedgerActual, Dimension: dimension, SourceRef: "run-1/1"},
			{Ordinal: 2, Account: "2101", Side: paygldomain.SideCredit, Amount: amount, Currency: "USD", Entity: "entity-1", Ledger: paygldomain.LedgerActual, Dimension: dimension, SourceRef: "run-1/1"},
		},
	})
	if err != nil {
		t.Fatalf("NewJournal: %v", err)
	}
	return journal
}

// TestTodo_PAYGL_005_Served proves correction is reachable through the
// application surface and remains append-only.
func TestTodo_PAYGL_005_Served(t *testing.T) {
	var app application.App
	surface := app.PayGL()
	if surface.CorrectJournal == nil {
		t.Fatal("composed application omitted PAYGL correction")
	}
	original := servedPayGLJournal(t)
	correction, err := surface.CorrectJournal(original, paygldomain.CorrectionRequest{
		CorrectionID: "journal-served-1-c1", Reason: "reclassify cost center",
		Reversals: []paygldomain.ReversalLine{{CorrectsOrdinal: 1}, {CorrectsOrdinal: 2}},
	})
	if err != nil {
		t.Fatalf("served correction: %v", err)
	}
	if correction.OriginalDigest != original.Digest || correction.Corrected.JournalID == original.JournalID {
		t.Fatalf("served correction did not preserve append-only lineage: %+v", correction)
	}
}

// TestTodo_PAYGL_006_Served proves reconciliation is reachable through the
// application surface and settles only after full line agreement.
func TestTodo_PAYGL_006_Served(t *testing.T) {
	var app application.App
	surface := app.PayGL()
	if surface.ReconcilePosting == nil {
		t.Fatal("composed application omitted PAYGL reconciliation")
	}
	journal := servedPayGLJournal(t)
	amount := journal.Lines[0].Amount
	obs := paygldomain.ERPObservation{
		Source: "erp-adapter/prod", BatchID: "batch-served-1", JournalDigest: journal.Digest,
		Currency: "USD", Total: journal.TotalDebits,
		Lines: []paygldomain.ERPLine{
			{JournalOrdinal: 1, ExternalID: "erp-1", Amount: amount, Currency: "USD", Dimensions: []string{"cc-1"}, State: paygldomain.ERPLineAccepted},
			{JournalOrdinal: 2, ExternalID: "erp-2", Amount: amount, Currency: "USD", Dimensions: []string{"cc-1"}, State: paygldomain.ERPLineAccepted},
		},
	}
	reconciliation, err := surface.ReconcilePosting(journal, obs)
	if err != nil {
		t.Fatalf("served reconciliation: %v", err)
	}
	if reconciliation.Aggregate != paygldomain.PostingSettled || len(reconciliation.Repairs) != 0 {
		t.Fatalf("served reconciliation = %+v, want settled without repairs", reconciliation)
	}
}

func TestServedPayGLNilApplication(t *testing.T) {
	var app *application.App
	if surface := app.PayGL(); surface.CorrectJournal != nil || surface.ReconcilePosting != nil {
		t.Fatal("nil application exposed PAYGL capabilities")
	}
}
