package settlement

import (
	"errors"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func settle007Input() FundReconInput {
	dec := func(s string) values.Decimal {
		return values.MustDecimal(s, 2, values.RoundingHalfUp)
	}
	return FundReconInput{
		Tenant: "acme", RunRef: "payrun-2026-03",
		FundingTotal: dec("3000.00"),
		Instructions: []InstructionLine{
			{Ref: "instruction-1", Amount: dec("1250.00")},
			{Ref: "instruction-2", Amount: dec("1750.00")},
		},
		Settlements: []SettlementLine{
			{Ref: "instruction-1", Kind: ObservationSettlement, Amount: dec("1250.00")},
			{Ref: "instruction-2", Kind: ObservationSettlement, Amount: dec("1750.00")},
		},
		Now: settle006At,
	}
}

// TestTodo_SETTLE_007 is the PRIMARY contract: totals and per-payment
// states classify exact settled, rejected, returned, pending and unknown
// deltas, and payroll completion stays blocked until policy permits.
// Acceptance presented as settlement, or funding and return totals that
// mismatch, return SETTLE_007_REJECTED with field/state/version and
// persist nothing.
func TestTodo_SETTLE_007(t *testing.T) {
	got, err := ReconcileFunding(settle007Input())
	if err != nil {
		t.Fatalf("ReconcileFunding: %v", err)
	}
	if len(got.Payments) != 2 || got.SettledTotal.String() != "3000.00" {
		t.Fatalf("full settlement must total exactly: %+v", got)
	}
	if !got.CompletionAllowed || got.FundingDelta.String() != "0.00" || got.Digest == "" {
		t.Fatalf("funded settlement must complete with zero delta and a digest: %+v", got)
	}

	t.Run("acceptance is not settlement", func(t *testing.T) {
		in := settle007Input()
		in.Settlements[0].Kind = ObservationAcceptance
		before := append([]InstructionLine(nil), in.Instructions...)
		_, err := ReconcileFunding(in)
		var rej *FundReconRejection
		if !errors.As(err, &rej) || !errors.Is(err, ErrFundReconRejected) {
			t.Fatalf("acceptance-as-settlement must be SETTLE_007_REJECTED, got %v", err)
		}
		if rej.Field == "" || rej.State == "" || rej.Version == "" {
			t.Fatalf("rejection must name field/state/version: %+v", rej)
		}
		if len(in.Instructions) != len(before) {
			t.Fatalf("refusal must persist nothing")
		}
	})

	t.Run("funding total mismatch is refused", func(t *testing.T) {
		in := settle007Input()
		in.FundingTotal = values.MustDecimal("2999.99", 2, values.RoundingHalfUp)
		if _, err := ReconcileFunding(in); !errors.Is(err, ErrFundReconRejected) {
			t.Fatalf("funding mismatch must be SETTLE_007_REJECTED, got %v", err)
		}
	})

	t.Run("return amount mismatch is refused", func(t *testing.T) {
		in := settle007Input()
		in.Settlements[1] = SettlementLine{Ref: "instruction-2", Kind: ObservationReturn, Amount: values.MustDecimal("1700.00", 2, values.RoundingHalfUp)}
		if _, err := ReconcileFunding(in); !errors.Is(err, ErrFundReconRejected) {
			t.Fatalf("return mismatch must be SETTLE_007_REJECTED, got %v", err)
		}
	})

	t.Run("pending and returned block completion", func(t *testing.T) {
		in := settle007Input()
		in.Settlements = []SettlementLine{
			{Ref: "instruction-1", Kind: ObservationSettlement, Amount: values.MustDecimal("1250.00", 2, values.RoundingHalfUp)},
		}
		got, err := ReconcileFunding(in)
		if err != nil {
			t.Fatal(err)
		}
		if got.Payments[1].Fate != FatePending || got.CompletionAllowed {
			t.Fatalf("pending must block completion: %+v", got)
		}
		if got.PendingTotal.String() != "1750.00" {
			t.Fatalf("pending total must be exact: %+v", got)
		}
		ret := settle007Input()
		ret.Settlements[0] = SettlementLine{Ref: "instruction-1", Kind: ObservationReturn, Amount: values.MustDecimal("1250.00", 2, values.RoundingHalfUp)}
		got, err = ReconcileFunding(ret)
		if err != nil {
			t.Fatal(err)
		}
		if got.Payments[0].Fate != FateReturned || got.CompletionAllowed {
			t.Fatalf("returned must block completion: %+v", got)
		}
	})
}

func TestTodo_SETTLE_007_Race(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := ReconcileFunding(settle007Input())
			if err != nil {
				t.Error(err)
				return
			}
			if !got.CompletionAllowed || got.SettledTotal.String() != "3000.00" {
				t.Errorf("concurrent reconcile diverged: %+v", got)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_SETTLE_007_Integration(t *testing.T) {
	// A return observed on the rail composes with the SETTLE-006
	// corrective: the reconciled RETURNED fate matches the corrective
	// kind for the same instruction and amount.
	in := settle007Input()
	in.Settlements[0] = SettlementLine{Ref: "instruction-1", Kind: ObservationReturn, Amount: values.MustDecimal("1250.00", 2, values.RoundingHalfUp)}
	got, err := ReconcileFunding(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Payments[0].Fate != FateReturned || got.ReturnedTotal.String() != "1250.00" {
		t.Fatalf("rail return must reconcile as RETURNED: %+v", got)
	}
	corrective, err := ApplyCorrectiveIntent(settle006Intent())
	if err != nil {
		t.Fatal(err)
	}
	if corrective.Kind != CorrectiveReturn || corrective.ReportingDelta.String() != got.Payments[0].Amount.String() {
		t.Fatalf("corrective and reconciliation must agree on the returned amount")
	}
}

func TestTodo_SETTLE_007_Fault(t *testing.T) {
	dup := settle007Input()
	dup.Settlements = append(dup.Settlements, SettlementLine{Ref: "instruction-1", Kind: ObservationSettlement, Amount: values.MustDecimal("1250.00", 2, values.RoundingHalfUp)})
	if _, err := ReconcileFunding(dup); !errors.Is(err, ErrFundReconRejected) {
		t.Fatalf("competing observations must be SETTLE_007_REJECTED")
	}
	indeterminate := settle007Input()
	indeterminate.Settlements[0].Kind = ObservationIndeterminate
	got, err := ReconcileFunding(indeterminate)
	if err != nil {
		t.Fatal(err)
	}
	if got.Payments[0].Fate != FateUnknown || got.CompletionAllowed {
		t.Fatalf("indeterminate evidence must be UNKNOWN and block: %+v", got)
	}
	tolerant := settle007Input()
	tolerant.Settlements = []SettlementLine{
		{Ref: "instruction-1", Kind: ObservationSettlement, Amount: values.MustDecimal("1250.00", 2, values.RoundingHalfUp)},
	}
	tolerant.Policy.AllowPendingCompletion = true
	got, err = ReconcileFunding(tolerant)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CompletionAllowed {
		t.Fatalf("tolerant policy must permit completion with pending: %+v", got)
	}
}

func TestTodo_SETTLE_007_Mutation(t *testing.T) {
	a, err := ReconcileFunding(settle007Input())
	if err != nil {
		t.Fatal(err)
	}
	in := settle007Input()
	in.Settlements[1].Kind = ObservationRejection
	b, err := ReconcileFunding(in)
	if err != nil {
		t.Fatal(err)
	}
	if b.Payments[1].Fate != FateRejected || b.Digest == a.Digest {
		t.Fatalf("rejection must reclassify and move the digest: %+v", b)
	}
	if b.RejectedTotal.String() != "1750.00" || b.CompletionAllowed {
		t.Fatalf("rejected totals must be exact and block: %+v", b)
	}
	bad := settle007Input()
	bad.Instructions[0].Ref = "instruction-2"
	if _, err := ReconcileFunding(bad); !errors.Is(err, ErrFundReconRejected) {
		t.Fatalf("duplicated instruction ref must be SETTLE_007_REJECTED")
	}
}
