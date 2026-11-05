package accounting

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payable"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/workorderbilling"
)

func acctOp() AcctOp {
	return AcctOp{TenantID: "tenant-a", LegalEntityID: "le-1", ActorID: "ingester-1", IngestAllowed: true, ReviewAllowed: true}
}

func acctRegistry() (*InboundRegistry, *workorderbilling.ReceivableLedger) {
	now := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	ap := payable.NewPayableRegistry(func() time.Time { return now })
	registry := NewInboundRegistry(func() time.Time { return now }, ap)
	ledger := workorderbilling.NewReceivableLedger("tenant-a")
	if err := registry.BindLedger(acctOp(), ledger); err != nil {
		panic(err)
	}
	return registry, ledger
}

func acctAdmitAR(t *testing.T, registry *InboundRegistry) {
	t.Helper()
	op := acctOp()
	for _, kind := range []string{SourceARInvoice, SourceARReceipt} {
		if _, err := registry.AdmitSource(op, SourceProfile{
			ConnectionID: "conn-ar", AccountID: "acct-ar-1", Kind: kind,
			Module: "billing", Enabled: true, SettlesCash: true,
		}, "admit-"+kind); err != nil {
			t.Fatal(err)
		}
	}
}

func acctInvoiceObs() AccountingObservation {
	return AccountingObservation{
		ConnectionID: "conn-ar", AccountID: "acct-ar-1", Kind: SourceARInvoice,
		ObjectID: "inv-60", ExternalTextID: "textID42", EventID: "evt-inv-60-v1",
		Version: 1, Digest: "sha256:inv60v1",
		ObservedAt: "2026-10-22T09:00:00Z", EffectiveAt: "2026-10-22T09:00:00Z",
		Status: "open", Amount: "60.00 USD",
		Authority: "controller-1", AuthorityAt: "2026-10-22T08:00:00Z", AuthorityDecision: "approved",
	}
}

func acctReceiptObs(version uint64, event, digest, status, amount, settlement string) AccountingObservation {
	return AccountingObservation{
		ConnectionID: "conn-ar", AccountID: "acct-ar-1", Kind: SourceARReceipt,
		ObjectID: "inv-60", ExternalTextID: "textID42", EventID: event,
		Version: version, Digest: digest,
		ObservedAt: "2026-10-22T10:00:00Z", EffectiveAt: "2026-10-22T10:00:00Z",
		Status: status, Amount: amount, SettlementRef: settlement,
		Authority: "controller-1", AuthorityAt: "2026-10-22T08:00:00Z", AuthorityDecision: "approved",
	}
}

// TestTodo_SMB_ACCT_005 pins the headline inbound oracle: provider event v3
// paid followed by v2 open never regresses the accepted v3 observation, and a
// duplicate v3 leaves balances unchanged.
func TestTodo_SMB_ACCT_005(t *testing.T) {
	registry, ledger := acctRegistry()
	op := acctOp()
	acctAdmitAR(t, registry)

	invoice, err := registry.Ingest(op, acctInvoiceObs(), "acct5-invoice")
	if err != nil {
		t.Fatal(err)
	}
	if invoice.Status != OutcomeApplied {
		t.Fatalf("invoice outcome = %+v, want applied", invoice)
	}
	projection, err := ledger.Explain("ar-inv-60", "2026-10-22T11:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if projection.Remaining.String() != "60.00 USD" {
		t.Fatalf("AR remaining = %s, want 60.00 USD", projection.Remaining.String())
	}

	paid, err := registry.Ingest(op, acctReceiptObs(3, "evt-rcpt-60-v3", "sha256:rcpt60v3", "paid", "60.00 USD", "settle-ar-1"), "acct5-receipt-v3")
	if err != nil {
		t.Fatal(err)
	}
	if paid.Status != OutcomeApplied {
		t.Fatalf("receipt v3 outcome = %+v, want applied", paid)
	}
	projection, err = ledger.Explain("ar-inv-60", "2026-10-22T11:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if projection.Remaining.String() != "0.00 USD" {
		t.Fatalf("AR remaining after v3 paid = %s, want 0.00 USD", projection.Remaining.String())
	}

	if _, err := registry.Ingest(op, acctReceiptObs(2, "evt-rcpt-60-v2", "sha256:rcpt60v2", "open", "60.00 USD", ""), "acct5-receipt-v2"); !errors.Is(err, ErrAcctStale) {
		t.Fatalf("stale v2 err = %v, want ErrAcctStale, never a regress", err)
	}
	projection, err = ledger.Explain("ar-inv-60", "2026-10-22T11:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if projection.Remaining.String() != "0.00 USD" {
		t.Fatalf("AR remaining after stale v2 = %s, want 0.00 USD", projection.Remaining.String())
	}

	dup, err := registry.Ingest(op, acctReceiptObs(3, "evt-rcpt-60-v3", "sha256:rcpt60v3", "paid", "60.00 USD", "settle-ar-1"), "acct5-receipt-v3-dup")
	if err != nil {
		t.Fatal(err)
	}
	if dup.Status != OutcomeDuplicate || dup.ReceiptID != paid.ReceiptID {
		t.Fatalf("duplicate v3 = %+v, want duplicate of %s", dup, paid.ReceiptID)
	}
	projection, err = ledger.Explain("ar-inv-60", "2026-10-22T11:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if projection.Remaining.String() != "0.00 USD" {
		t.Fatalf("AR remaining after duplicate v3 = %s, want 0.00 USD", projection.Remaining.String())
	}
}

func TestTodo_SMB_ACCT_005_Unit(t *testing.T) {
	t.Run("payroll kind never admitted", func(t *testing.T) {
		registry, _ := acctRegistry()
		if _, err := registry.AdmitSource(acctOp(), SourceProfile{
			ConnectionID: "conn-ar", AccountID: "acct-ar-1", Kind: "PAYROLL_GAIN",
			Module: "payroll", Enabled: true,
		}, "admit-paygl"); !errors.Is(err, ErrAcctValidation) {
			t.Fatalf("PAYGL admit err = %v, want validation refusal", err)
		}
	})

	t.Run("disabled module quarantines without effects", func(t *testing.T) {
		registry, ledger := acctRegistry()
		op := acctOp()
		if _, err := registry.AdmitSource(op, SourceProfile{
			ConnectionID: "conn-ap", AccountID: "acct-ap-9", Kind: SourceAPPayment,
			Module: "payables", Enabled: false,
		}, "admit-disabled"); err != nil {
			t.Fatal(err)
		}
		outcome, err := registry.Ingest(op, AccountingObservation{
			ConnectionID: "conn-ap", AccountID: "acct-ap-9", Kind: SourceAPPayment,
			ObjectID: "bill-60", ExternalTextID: "textID42", EventID: "evt-disabled",
			Version: 1, Digest: "sha256:disabled",
			ObservedAt: "2026-10-22T09:00:00Z", EffectiveAt: "2026-10-22T09:00:00Z",
			Status: "paid", Amount: "20.00 USD", SettlementRef: "settle-x",
			Authority: "controller-1", AuthorityAt: "2026-10-22T08:00:00Z", AuthorityDecision: "approved",
		}, "ingest-disabled")
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Status != OutcomeQuarantined {
			t.Fatalf("disabled outcome = %+v, want quarantined", outcome)
		}
		if _, err := ledger.Explain("ar-inv-60", "2026-10-22T11:00:00Z"); err == nil {
			t.Fatal("disabled-module observation leaked into the AR ledger")
		}
	})

	t.Run("paid without settlement stays externally reported", func(t *testing.T) {
		registry, ledger := acctRegistry()
		op := acctOp()
		acctAdmitAR(t, registry)
		if _, err := registry.Ingest(op, acctInvoiceObs(), "ext-invoice"); err != nil {
			t.Fatal(err)
		}
		outcome, err := registry.Ingest(op, acctReceiptObs(4, "evt-rcpt-60-v4", "sha256:rcpt60v4", "paid", "60.00 USD", ""), "ext-receipt")
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Status != OutcomeExternal {
			t.Fatalf("no-evidence outcome = %+v, want externally_reported", outcome)
		}
		projection, err := ledger.Explain("ar-inv-60", "2026-10-22T11:00:00Z")
		if err != nil {
			t.Fatal(err)
		}
		if projection.Remaining.String() != "60.00 USD" {
			t.Fatalf("AR remaining = %s, want 60.00 USD untouched", projection.Remaining.String())
		}
	})

	t.Run("external update keeps local notes", func(t *testing.T) {
		registry, _ := acctRegistry()
		op := acctOp()
		acctAdmitAR(t, registry)
		first := acctInvoiceObs()
		first.LocalNotes = "collector: call Tuesday"
		admitted, err := registry.Ingest(op, first, "notes-v1")
		if err != nil {
			t.Fatal(err)
		}
		second := acctInvoiceObs()
		second.Version = 2
		second.EventID = "evt-inv-60-v2"
		second.Digest = "sha256:inv60v2"
		second.Status = "open"
		second.LocalNotes = "external overwrites notes"
		if _, err := registry.Ingest(op, second, "notes-v2"); err != nil {
			t.Fatal(err)
		}
		family, err := registry.ExplainFamily(op, admitted.FamilyID)
		if err != nil {
			t.Fatal(err)
		}
		if family.Notes != "collector: call Tuesday" {
			t.Fatalf("family notes = %q, want local notes retained", family.Notes)
		}
		if family.Version != 2 {
			t.Fatalf("family version = %d, want 2", family.Version)
		}
	})

	t.Run("idempotency key conflict on changed payload", func(t *testing.T) {
		registry, _ := acctRegistry()
		op := acctOp()
		acctAdmitAR(t, registry)
		first, err := registry.Ingest(op, acctInvoiceObs(), "same-key")
		if err != nil {
			t.Fatal(err)
		}
		replay, err := registry.Ingest(op, acctInvoiceObs(), "same-key")
		if err != nil {
			t.Fatal(err)
		}
		if replay.ReceiptID != first.ReceiptID {
			t.Fatalf("replay receipt = %s, want %s", replay.ReceiptID, first.ReceiptID)
		}
		changed := acctInvoiceObs()
		changed.Amount = "61.00 USD"
		if _, err := registry.Ingest(op, changed, "same-key"); !errors.Is(err, ErrAcctConflict) {
			t.Fatalf("changed payload err = %v, want ErrAcctConflict", err)
		}
	})

	t.Run("contradiction opens discrepancy", func(t *testing.T) {
		registry, _ := acctRegistry()
		op := acctOp()
		acctAdmitAR(t, registry)
		admitted, err := registry.Ingest(op, acctInvoiceObs(), "disc-v1")
		if err != nil {
			t.Fatal(err)
		}
		clash := acctInvoiceObs()
		clash.EventID = "evt-inv-60-v1b"
		clash.Digest = "sha256:inv60v1b"
		clash.Amount = "70.00 USD"
		outcome, err := registry.Ingest(op, clash, "disc-v1b")
		if err != nil {
			t.Fatal(err)
		}
		if outcome.Status != OutcomeQuarantined {
			t.Fatalf("clash outcome = %+v, want quarantined", outcome)
		}
		discrepancies, err := registry.Discrepancies(op)
		if err != nil {
			t.Fatal(err)
		}
		if len(discrepancies) != 1 || discrepancies[0].FamilyID != admitted.FamilyID {
			t.Fatalf("discrepancies = %+v, want one for the family", discrepancies)
		}
		if !strings.Contains(discrepancies[0].ProposedRepair, "70.00 USD") {
			t.Fatalf("repair = %q, want the proposed amount", discrepancies[0].ProposedRepair)
		}
	})
}
