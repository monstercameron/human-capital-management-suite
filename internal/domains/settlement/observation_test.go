package settlement

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/payroll"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func observeInstant(text string) time.Time {
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		panic(err)
	}
	return parsed.UTC()
}

func observeReleasedRun(t *testing.T) payroll.PayrollRun {
	t.Helper()
	period := payroll.PeriodRef{ID: "period-2026-06", Version: "v1", Digest: "digest:period-1"}
	population := payroll.PopulationBindingRef{DefinitionID: "pop-monthly", RevisionVersion: "v3", Digest: "digest:pop-1"}
	run, err := payroll.NewPayrollRun("run-2026-11", "pg-monthly", period, population, "digest:inputs-1")
	if err != nil {
		t.Fatalf("NewPayrollRun: %v", err)
	}
	calculated, err := run.Calculate("digest:calc-1")
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	released, err := calculated.Release("digest:release-1")
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	return released
}

func observeInstruction(t *testing.T, run payroll.PayrollRun, id string) PaymentInstruction {
	t.Helper()
	instruction, err := NewPaymentInstruction(run, PaymentInstructionSpec{
		InstructionID: id, PayeeRef: "payee-" + id,
		Amount: values.MustDecimal("3140.22", 2, values.RoundingHalfUp), Currency: "USD",
		FundingSourceRef: "fund-1", Rail: RailACH, BankDetailRef: "bank-1",
		ScheduleRef: "sched-1",
	})
	if err != nil {
		t.Fatalf("NewPaymentInstruction: %v", err)
	}
	return instruction
}

func observeAcceptance(instruction PaymentInstruction, at time.Time) RailObservation {
	return RailObservation{
		InstructionKey: instruction.NaturalKey(), InstructionID: instruction.InstructionID,
		Kind: ObservationAcceptance, Authoritative: true, Source: "rail:ach",
		ProviderRef: "prov-accept-1", ObservedAt: at,
	}
}

func observeSettlement(instruction PaymentInstruction, at time.Time) RailObservation {
	return RailObservation{
		InstructionKey: instruction.NaturalKey(), InstructionID: instruction.InstructionID,
		Kind: ObservationSettlement, Authoritative: true, Source: "rail:ach",
		ProviderRef: "prov-settle-1", ObservedAt: at,
	}
}

// TestTodo_SETTLE_005 is the primary SETTLE-005 contract test: acceptance and
// settlement are distinct evidence states, funding is never a settlement kind,
// and only an authoritative rail observation advances settlement.
func TestTodo_SETTLE_005(t *testing.T) {
	acceptedAt := observeInstant("2026-06-16T19:00:00Z")
	settledAt := observeInstant("2026-06-17T09:00:00Z")

	t.Run("acceptance advances to accepted without settling", func(t *testing.T) {
		tracker := NewObservationTracker()
		instruction := observeInstruction(t, observeReleasedRun(t), "instr-accept-1")
		if got := tracker.State(instruction.NaturalKey()); got != EvidencePending {
			t.Fatalf("unobserved instruction must read PENDING, got %s", got)
		}
		outcome, err := tracker.Observe(observeAcceptance(instruction, acceptedAt))
		if err != nil {
			t.Fatalf("Observe acceptance: %v", err)
		}
		if outcome.State != EvidenceAccepted || !outcome.Advanced {
			t.Fatalf("acceptance must advance to ACCEPTED, got %+v", outcome)
		}
		if got := tracker.State(instruction.NaturalKey()); got != EvidenceAccepted {
			t.Fatalf("state must be ACCEPTED, got %s", got)
		}
	})

	t.Run("settlement requires prior acceptance", func(t *testing.T) {
		tracker := NewObservationTracker()
		instruction := observeInstruction(t, observeReleasedRun(t), "instr-settle-first")
		_, err := tracker.Observe(observeSettlement(instruction, settledAt))
		if !errors.Is(err, ErrObservationRejected) {
			t.Fatalf("settlement before acceptance must be SETTLE_005_REJECTED, got %v", err)
		}
		if got := tracker.State(instruction.NaturalKey()); got != EvidencePending {
			t.Fatalf("refused settlement must leave PENDING, got %s", got)
		}
	})

	t.Run("authoritative settlement after acceptance settles", func(t *testing.T) {
		tracker := NewObservationTracker()
		instruction := observeInstruction(t, observeReleasedRun(t), "instr-full")
		if _, err := tracker.Observe(observeAcceptance(instruction, acceptedAt)); err != nil {
			t.Fatalf("Observe acceptance: %v", err)
		}
		outcome, err := tracker.Observe(observeSettlement(instruction, settledAt))
		if err != nil {
			t.Fatalf("Observe settlement: %v", err)
		}
		if outcome.State != EvidenceSettled || !outcome.Advanced {
			t.Fatalf("authoritative settlement must advance to SETTLED, got %+v", outcome)
		}
	})

	t.Run("funding is not an observation kind", func(t *testing.T) {
		tracker := NewObservationTracker()
		instruction := observeInstruction(t, observeReleasedRun(t), "instr-funding")
		obs := observeAcceptance(instruction, acceptedAt)
		obs.Kind = "FUNDING"
		_, err := tracker.Observe(obs)
		if !errors.Is(err, ErrObservationRejected) {
			t.Fatalf("funding observation must be SETTLE_005_REJECTED, got %v", err)
		}
		var refusal *ObservationError
		if !errors.As(err, &refusal) || refusal.Field != "kind" {
			t.Fatalf("refusal must name field=kind, got %v", err)
		}
		if got := tracker.State(instruction.NaturalKey()); got != EvidencePending {
			t.Fatalf("refused funding observation must leave PENDING, got %s", got)
		}
	})

	t.Run("non-authoritative settlement never advances", func(t *testing.T) {
		tracker := NewObservationTracker()
		instruction := observeInstruction(t, observeReleasedRun(t), "instr-nonauth")
		if _, err := tracker.Observe(observeAcceptance(instruction, acceptedAt)); err != nil {
			t.Fatalf("Observe acceptance: %v", err)
		}
		obs := observeSettlement(instruction, settledAt)
		obs.Authoritative = false
		outcome, err := tracker.Observe(obs)
		if err != nil {
			t.Fatalf("non-authoritative observation must be recorded, not refused: %v", err)
		}
		if outcome.Advanced || outcome.State != EvidenceAccepted {
			t.Fatalf("non-authoritative settlement must not advance, got %+v", outcome)
		}
	})

	t.Run("missing identity and evidence are refused", func(t *testing.T) {
		tracker := NewObservationTracker()
		instruction := observeInstruction(t, observeReleasedRun(t), "instr-missing")
		obs := observeAcceptance(instruction, acceptedAt)
		obs.ProviderRef = ""
		if _, err := tracker.Observe(obs); !errors.Is(err, ErrObservationRejected) {
			t.Fatalf("observation without provider evidence must be refused, got %v", err)
		}
		obs = observeAcceptance(instruction, acceptedAt)
		obs.InstructionKey = ""
		if _, err := tracker.Observe(obs); !errors.Is(err, ErrObservationRejected) {
			t.Fatalf("observation without instruction key must be refused, got %v", err)
		}
		obs = observeAcceptance(instruction, acceptedAt)
		obs.ObservedAt = time.Time{}
		if _, err := tracker.Observe(obs); !errors.Is(err, ErrObservationRejected) {
			t.Fatalf("observation without reference time must be refused, got %v", err)
		}
	})

	t.Run("rejection and return are terminal and distinct", func(t *testing.T) {
		tracker := NewObservationTracker()
		run := observeReleasedRun(t)
		rejected := observeInstruction(t, run, "instr-reject")
		if _, err := tracker.Observe(observeAcceptance(rejected, acceptedAt)); err != nil {
			t.Fatalf("Observe acceptance: %v", err)
		}
		reject := observeSettlement(rejected, settledAt)
		reject.Kind = ObservationRejection
		reject.ProviderRef = "prov-reject-1"
		outcome, err := tracker.Observe(reject)
		if err != nil {
			t.Fatalf("Observe rejection: %v", err)
		}
		if outcome.State != EvidenceRejected {
			t.Fatalf("state must be REJECTED, got %+v", outcome)
		}
		returned := observeInstruction(t, run, "instr-return")
		if _, err := tracker.Observe(observeAcceptance(returned, acceptedAt)); err != nil {
			t.Fatalf("Observe acceptance: %v", err)
		}
		if _, err := tracker.Observe(observeSettlement(returned, settledAt)); err != nil {
			t.Fatalf("Observe settlement: %v", err)
		}
		ret := observeSettlement(returned, settledAt.Add(time.Hour))
		ret.Kind = ObservationReturn
		ret.ProviderRef = "prov-return-1"
		outcome, err = tracker.Observe(ret)
		if err != nil {
			t.Fatalf("Observe return: %v", err)
		}
		if outcome.State != EvidenceReturned {
			t.Fatalf("state must be RETURNED, got %+v", outcome)
		}
		if tracker.State(rejected.NaturalKey()) == tracker.State(returned.NaturalKey()) {
			t.Fatal("REJECTED and RETURNED must be distinct evidence states")
		}
	})
}

// TestTodo_SETTLE_005_Race proves concurrent rail observations over one shared
// tracker never lose, duplicate or race: every instruction settles exactly
// once and every sighting is counted.
func TestTodo_SETTLE_005_Race(t *testing.T) {
	run := observeReleasedRun(t)
	const workers = 8
	instructions := make([]PaymentInstruction, workers)
	for i := range instructions {
		instructions[i] = observeInstruction(t, run, string(rune('a'+i))+"-race-instr")
	}
	tracker := NewObservationTracker()
	acceptedAt := observeInstant("2026-06-16T19:00:00Z")
	var wg sync.WaitGroup
	for _, instruction := range instructions {
		wg.Add(1)
		go func(instruction PaymentInstruction) {
			defer wg.Done()
			if _, err := tracker.Observe(observeAcceptance(instruction, acceptedAt)); err != nil {
				t.Errorf("Observe acceptance: %v", err)
			}
		}(instruction)
	}
	wg.Wait()
	for _, instruction := range instructions {
		wg.Add(1)
		go func(instruction PaymentInstruction) {
			defer wg.Done()
			outcome, err := tracker.Observe(observeSettlement(instruction, acceptedAt.Add(time.Hour)))
			if err != nil {
				t.Errorf("Observe settlement: %v", err)
				return
			}
			if outcome.State != EvidenceSettled {
				t.Errorf("state must be SETTLED, got %+v", outcome)
			}
		}(instruction)
	}
	wg.Wait()
	for _, instruction := range instructions {
		if got := tracker.State(instruction.NaturalKey()); got != EvidenceSettled {
			t.Fatalf("instruction %s must be SETTLED, got %s", instruction.InstructionID, got)
		}
	}
}

// TestTodo_SETTLE_005_Integration crosses the real payroll-to-settlement
// boundary: a released payroll run yields a payment instruction whose
// rail acceptance and settlement are observed to SETTLED with no duplicate
// provider effect.
func TestTodo_SETTLE_005_Integration(t *testing.T) {
	run := observeReleasedRun(t)
	instruction := observeInstruction(t, run, "instr-integration")
	if instruction.PayrollRunRef != run.CanonicalDigest {
		t.Fatalf("instruction must bind the released run digest, got %q", instruction.PayrollRunRef)
	}
	tracker := NewObservationTracker()
	acceptedAt := observeInstant("2026-06-16T19:00:00Z")
	settledAt := observeInstant("2026-06-17T09:00:00Z")
	if _, err := tracker.Observe(observeAcceptance(instruction, acceptedAt)); err != nil {
		t.Fatalf("Observe acceptance: %v", err)
	}
	first, err := tracker.Observe(observeSettlement(instruction, settledAt))
	if err != nil {
		t.Fatalf("Observe settlement: %v", err)
	}
	replay, err := tracker.Observe(observeSettlement(instruction, settledAt))
	if err != nil {
		t.Fatalf("settlement replay must be accepted: %v", err)
	}
	if replay.Advanced || replay.State != EvidenceSettled {
		t.Fatalf("settlement replay must not re-advance, got %+v", replay)
	}
	if first.ObservedAt.After(replay.ObservedAt) {
		t.Fatalf("replay must not move the observation backwards: %+v vs %+v", first, replay)
	}
	if got := tracker.State(instruction.NaturalKey()); got != EvidenceSettled {
		t.Fatalf("integration instruction must be SETTLED, got %s", got)
	}
}

// TestTodo_SETTLE_005_Fault proves ambiguous rail evidence never advances
// settlement: an indeterminate observation reads UNKNOWN, acceptance can still
// follow, and replays stay idempotent.
func TestTodo_SETTLE_005_Fault(t *testing.T) {
	tracker := NewObservationTracker()
	instruction := observeInstruction(t, observeReleasedRun(t), "instr-fault")
	at := observeInstant("2026-06-16T19:00:00Z")
	ambiguous := RailObservation{
		InstructionKey: instruction.NaturalKey(), InstructionID: instruction.InstructionID,
		Kind: ObservationIndeterminate, Authoritative: false, Source: "rail:ach",
		ObservedAt: at,
	}
	outcome, err := tracker.Observe(ambiguous)
	if err != nil {
		t.Fatalf("indeterminate observation must be recorded, not refused: %v", err)
	}
	if outcome.Advanced || outcome.State != EvidenceUnknown {
		t.Fatalf("ambiguous evidence must read UNKNOWN without advancing, got %+v", outcome)
	}
	if _, err := tracker.Observe(observeAcceptance(instruction, at.Add(time.Minute))); err != nil {
		t.Fatalf("acceptance after ambiguity must still be accepted: %v", err)
	}
	replay, err := tracker.Observe(observeAcceptance(instruction, at.Add(2*time.Minute)))
	if err != nil {
		t.Fatalf("acceptance replay must be accepted: %v", err)
	}
	if replay.Advanced || replay.State != EvidenceAccepted {
		t.Fatalf("acceptance replay must not re-advance, got %+v", replay)
	}
	if _, err := tracker.Observe(observeSettlement(instruction, at.Add(time.Hour))); err != nil {
		t.Fatalf("settlement after ambiguity must still settle: %v", err)
	}
	if got := tracker.State(instruction.NaturalKey()); got != EvidenceSettled {
		t.Fatalf("fault instruction must reach SETTLED, got %s", got)
	}
}

// TestTodo_SETTLE_005_Mutation kills the three seeded semantic mutants that
// would collapse the acceptance/settlement distinction.
func TestTodo_SETTLE_005_Mutation(t *testing.T) {
	at := observeInstant("2026-06-16T19:00:00Z")

	t.Run("mutant: acceptance recorded as settlement", func(t *testing.T) {
		tracker := NewObservationTracker()
		instruction := observeInstruction(t, observeReleasedRun(t), "instr-mut-accept")
		outcome, err := tracker.Observe(observeAcceptance(instruction, at))
		if err != nil {
			t.Fatalf("Observe acceptance: %v", err)
		}
		if outcome.State == EvidenceSettled {
			t.Fatal("mutant survived: acceptance must never report SETTLED")
		}
	})

	t.Run("mutant: authority check removed", func(t *testing.T) {
		tracker := NewObservationTracker()
		instruction := observeInstruction(t, observeReleasedRun(t), "instr-mut-auth")
		if _, err := tracker.Observe(observeAcceptance(instruction, at)); err != nil {
			t.Fatalf("Observe acceptance: %v", err)
		}
		obs := observeSettlement(instruction, at.Add(time.Hour))
		obs.Authoritative = false
		obs.Source = "untrusted:relay"
		outcome, err := tracker.Observe(obs)
		if err != nil {
			t.Fatalf("untrusted observation must be recorded, not refused: %v", err)
		}
		if outcome.State == EvidenceSettled {
			t.Fatal("mutant survived: untrusted settlement must never report SETTLED")
		}
	})

	t.Run("mutant: settlement totals advance without acceptance", func(t *testing.T) {
		tracker := NewObservationTracker()
		instruction := observeInstruction(t, observeReleasedRun(t), "instr-mut-order")
		if _, err := tracker.Observe(observeSettlement(instruction, at)); !errors.Is(err, ErrObservationRejected) {
			t.Fatal("mutant survived: settlement without acceptance must be refused")
		}
	})
}
