package settlement

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func settlementAmount(t *testing.T, text string) values.Decimal {
	t.Helper()
	d, err := values.NewDecimal(text, 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func releasedRun(t *testing.T) payroll.PayrollRun {
	t.Helper()
	run, err := payroll.NewPayrollRun("run-settle", "monthly",
		payroll.PeriodRef{ID: "period", Version: "v1", Digest: "sha256:period"},
		payroll.PopulationBindingRef{DefinitionID: "population", RevisionVersion: "v1", Digest: "sha256:population"}, "sha256:inputs")
	if err != nil {
		t.Fatal(err)
	}
	run, err = run.Calculate("sha256:calculation")
	if err != nil {
		t.Fatal(err)
	}
	run, err = run.Release("sha256:release")
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func instructionSpec(id string) PaymentInstructionSpec {
	worker := "worker:payee-1"
	destination := "bank-detail:token-1"
	return PaymentInstructionSpec{InstructionID: id, PayeeRef: worker, Amount: settlementAmountForTest("125.00"), Currency: "USD", FundingSourceRef: "funding:operating", Rail: RailACH, BankDetailRef: destination, ScheduleRef: "schedule:2026-11-30", PaymentMethodElection: settlementElection(worker, destination), SettlementPolicy: settlementPolicy()}
}

func settlementElection(worker, destination string) PaymentMethodElection {
	return PaymentMethodElection{ElectionID: "election:" + worker, WorkerRef: worker, Method: MethodDirectDeposit, Consented: true, ConsentEvidenceRef: "consent:" + worker, DestinationRef: destination, Jurisdiction: "US-CA", RulePackRef: "rules:ca-v1"}
}

func settlementPolicy() SettlementPolicy {
	return SettlementPolicy{Jurisdiction: "US-CA", RulePackRef: "rules:ca-v1", PaperCheckAllowed: true, PayCardAllowed: true}
}

func settlementAmountForTest(text string) values.Decimal {
	d, _ := values.NewDecimal(text, 2, values.RoundingExactRequired)
	return d
}

func validInstruction(t *testing.T) PaymentInstruction {
	t.Helper()
	i, err := NewPaymentInstruction(releasedRun(t), instructionSpec("instruction-1"))
	if err != nil {
		t.Fatal(err)
	}
	return i
}

// TestTodo_SETTLE_001 is the primary acceptance case: a released run creates
// a governed instruction and advances through immutable settlement revisions.
func TestTodo_SETTLE_001(t *testing.T) {
	i := validInstruction(t)
	states := []SettlementState{StateSubmitted, StateAcknowledged, StateSettled, StateReturned, StateReversed}
	evidence := []string{"provider:submitted", "provider:acknowledged", "rail:settled", "rail:returned", "ledger:reversed"}
	for n, state := range states {
		next, err := i.Transition(state, evidence[n])
		if err != nil {
			t.Fatalf("%s: %v", state, err)
		}
		if next.State != state || next.Revision != i.Revision+1 || next.SupersedesRevision != i.Revision || next.CanonicalDigest == i.CanonicalDigest {
			t.Fatalf("successor = %+v", next)
		}
		i = next
	}
	if i.State != StateReversed || i.Revision != 6 {
		t.Fatalf("final instruction = %+v", i)
	}
}

// TestTodo_SETTLE_001_Race proves immutable instruction reads are safe under
// concurrent callers.
func TestTodo_SETTLE_001_Race(t *testing.T) {
	i := validInstruction(t)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for n := 0; n < 16; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := i.Validate(); err != nil {
				errs <- err
			}
			if i.NaturalKey() == "" || i.CanonicalDigest == "" {
				errs <- errors.New("missing identity")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}

// TestTodo_SETTLE_001_Integration proves the instruction points at the exact
// released payroll revision rather than merely a worker or funding source.
func TestTodo_SETTLE_001_Integration(t *testing.T) {
	run := releasedRun(t)
	i, err := NewPaymentInstruction(run, instructionSpec("instruction-1"))
	if err != nil {
		t.Fatal(err)
	}
	if i.PayrollRunRef != run.CanonicalDigest || i.FundingSourceRef == "" || i.BankDetailRef == "" {
		t.Fatalf("instruction = %+v", i)
	}
	if i.NaturalKey() != i.IdempotencyKey() {
		t.Fatal("idempotency aliases disagree")
	}
}

// TestTodo_SETTLE_001_Fault covers missing release, invalid rail, and illegal
// lifecycle edges.
func TestTodo_SETTLE_001_Fault(t *testing.T) {
	run, err := payroll.NewPayrollRun("run", "monthly", payroll.PeriodRef{ID: "period", Version: "v1", Digest: "d"}, payroll.PopulationBindingRef{DefinitionID: "population", RevisionVersion: "v1", Digest: "p"}, "inputs")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewPaymentInstruction(run, instructionSpec("instruction-1")); !errors.Is(err, ErrNoReleasedPayrollRun) {
		t.Fatalf("unreleased run = %v", err)
	}
	spec := instructionSpec("instruction-2")
	spec.Rail = SettlementRail("CRYPTO")
	if _, err := NewPaymentInstruction(releasedRun(t), spec); !errors.Is(err, ErrInvalidPaymentInstruction) {
		t.Fatalf("unknown rail = %v", err)
	}
	missingElection := instructionSpec("instruction-no-election")
	missingElection.PaymentMethodElection = PaymentMethodElection{}
	if _, err := NewPaymentInstruction(releasedRun(t), missingElection); !errors.Is(err, ErrInvalidPaymentInstruction) {
		t.Fatalf("missing governed election = %v", err)
	}
	if _, err := validInstruction(t).Transition(StateSettled, "too-soon"); !errors.Is(err, ErrSettlementTransition) {
		t.Fatalf("illegal edge = %v", err)
	}
}

// TestTodo_SETTLE_001_Security proves raw bank material is rejected and is
// absent from the canonical audit narrative.
func TestTodo_SETTLE_001_Security(t *testing.T) {
	spec := instructionSpec("instruction-secure")
	spec.RawBankDetail = "4111111111111111"
	if _, err := NewPaymentInstruction(releasedRun(t), spec); !errors.Is(err, ErrInvalidPaymentInstruction) {
		t.Fatalf("raw bank detail = %v", err)
	}
	i := validInstruction(t)
	if strings.Contains(i.Explain(), "4111111111111111") {
		t.Fatalf("unsafe explanation = %q", i.Explain())
	}
}

// TestTodo_SETTLE_001_Conformance records the closed rail/state vocabularies
// and typed conformance refusal.
func TestTodo_SETTLE_001_Conformance(t *testing.T) {
	for _, rail := range []SettlementRail{RailACH, RailWire, RailSEPA, RailRTP, RailFedNow, RailInternal, RailCheck, RailPayCard} {
		if !rail.Valid() {
			t.Errorf("rail %s invalid", rail)
		}
	}
	if SettlementRail("OTHER").Valid() {
		t.Fatal("unknown rail accepted")
	}
	if SettlementState("ACCEPTED").Valid() {
		t.Fatal("provider acceptance became a settlement state")
	}
	bad := instructionSpec("instruction-bad")
	bad.BankDetailRef = ""
	if _, err := NewPaymentInstruction(releasedRun(t), bad); !errors.Is(err, ErrInvalidPaymentInstruction) {
		t.Fatalf("missing governed bank ref = %v", err)
	}
}

// TestTodo_SETTLE_001_Mutation proves natural-key idempotency and immutable
// lifecycle identity.
func TestTodo_SETTLE_001_Mutation(t *testing.T) {
	first := validInstruction(t)
	second, err := NewPaymentInstruction(releasedRun(t), instructionSpec("another-id"))
	if err != nil {
		t.Fatal(err)
	}
	if first.NaturalKey() != second.NaturalKey() {
		t.Fatal("natural key included instruction id")
	}
	existing, err := EnsureIdempotent(first, second)
	if err != nil || existing.CanonicalDigest != first.CanonicalDigest {
		t.Fatalf("idempotent result = %+v, err=%v", existing, err)
	}
	changedSpec := instructionSpec("different")
	changedSpec.Amount = settlementAmount(t, "126.00")
	changed, err := NewPaymentInstruction(releasedRun(t), changedSpec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureIdempotent(first, changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("natural-key conflict = %v", err)
	}
	if first.State != StateInstructed || first.Revision != 1 {
		t.Fatal("existing instruction mutated")
	}
}

// TestTodo_SETTLE_001_Explain checks the audit-safe explanation shape.
func TestTodo_SETTLE_001_Explain(t *testing.T) {
	i := validInstruction(t)
	if got := Explain(i); !strings.Contains(got, "INSTRUCTED") || !strings.Contains(got, "released run") {
		t.Fatalf("Explain = %q", got)
	}
}
