package settlement

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type fakeProvider struct {
	calls    atomic.Int64
	pending  *Journal
	behavior func(instruction PaymentInstruction) ProviderResponse
}

func (f *fakeProvider) Send(_ context.Context, instruction PaymentInstruction) (ProviderResponse, error) {
	f.calls.Add(1)
	if f.pending != nil {
		// The attempt must already be journaled before the provider is
		// observed: crash between send and response stays reconcilable.
		entry, ok := f.pending.Lookup(instruction.IdempotencyKey())
		if !ok || entry.Outcome != OutcomePending {
			panic("provider observed before the attempt was journaled")
		}
	}
	return f.behavior(instruction), nil
}

func settleInstruction(t *testing.T) PaymentInstruction {
	t.Helper()
	instruction := PaymentInstruction{
		InstructionID: "instr-1", PayrollRunRef: "run-1", PayeeRef: "payee-1",
		Amount: values.MustDecimal("100.00", 2, values.RoundingHalfUp), Currency: "USD",
		FundingSourceRef: "fund-1", Rail: RailACH, BankDetailRef: "bank-1",
		State: StateInstructed, Revision: 1,
	}
	instruction.CanonicalDigest = instruction.computedDigest()
	if err := instruction.Validate(); err != nil {
		t.Fatalf("fixture instruction must validate: %v", err)
	}
	return instruction
}

func settleLease(now time.Time) CredentialLease {
	return CredentialLease{
		LeaseRef: "lease-1", InstructionRef: "instr-1", Scope: "payments:submit",
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute),
	}
}

// TestTodo_SETTLE_004 is the primary SETTLE-004 contract test:
// timeout-after-send never triggers a blind duplicate, and instruction,
// credential lease, attempt and provider response are journaled before
// observation.
func TestTodo_SETTLE_004(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	t.Run("accepted submission journals every fact", func(t *testing.T) {
		journal := NewJournal()
		provider := &fakeProvider{pending: journal, behavior: func(PaymentInstruction) ProviderResponse {
			return ProviderResponse{Outcome: ProviderAccepted, ProviderRef: "prov-1"}
		}}
		entry, err := journal.Submit(context.Background(), SubmitRequest{
			Instruction: settleInstruction(t), Lease: settleLease(now), Now: now,
		}, provider)
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		if entry.Outcome != OutcomeAccepted || entry.Attempt != 1 {
			t.Fatalf("entry must record the first accepted attempt: %+v", entry)
		}
		if entry.LeaseRef != "lease-1" || entry.ProviderRef != "prov-1" || entry.InstructionDigest == "" {
			t.Fatalf("entry must journal lease, response and identity: %+v", entry)
		}
		if provider.calls.Load() != 1 {
			t.Fatalf("provider must be called exactly once, got %d", provider.calls.Load())
		}
	})

	t.Run("timeout stays ambiguous and never blind-duplicates", func(t *testing.T) {
		journal := NewJournal()
		provider := &fakeProvider{behavior: func(PaymentInstruction) ProviderResponse {
			return ProviderResponse{Outcome: ProviderTimeout, ProviderRef: ""}
		}}
		first, err := journal.Submit(context.Background(), SubmitRequest{
			Instruction: settleInstruction(t), Lease: settleLease(now), Now: now,
		}, provider)
		if err != nil {
			t.Fatalf("Submit: %v", err)
		}
		if first.Outcome != OutcomeAmbiguous {
			t.Fatalf("timeout must journal AMBIGUOUS, got %v", first.Outcome)
		}
		// A retry with the same key must NOT resend: reconcile instead.
		longLease := settleLease(now)
		longLease.ExpiresAt = now.Add(time.Hour)
		second, err := journal.Submit(context.Background(), SubmitRequest{
			Instruction: settleInstruction(t), Lease: longLease, Now: now.Add(time.Minute),
		}, provider)
		if err != nil {
			t.Fatalf("resubmit: %v", err)
		}
		if second.Attempt != 1 || provider.calls.Load() != 1 {
			t.Fatalf("ambiguous resubmit must not resend: %+v calls=%d", second, provider.calls.Load())
		}
		resolved, err := journal.Reconcile(ReconcileRequest{
			IdempotencyKey: settleInstruction(t).IdempotencyKey(),
			Outcome:        OutcomeAccepted, ProviderRef: "prov-late", Now: now.Add(2 * time.Minute),
		})
		if err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		if resolved.Outcome != OutcomeAccepted || resolved.ProviderRef != "prov-late" {
			t.Fatalf("reconciliation must confirm the late acceptance: %+v", resolved)
		}
	})

	t.Run("same key with different instruction conflicts", func(t *testing.T) {
		journal := NewJournal()
		provider := &fakeProvider{behavior: func(PaymentInstruction) ProviderResponse {
			return ProviderResponse{Outcome: ProviderAccepted, ProviderRef: "prov-1"}
		}}
		first := settleInstruction(t)
		if _, err := journal.Submit(context.Background(), SubmitRequest{Instruction: first, Lease: settleLease(now), Now: now}, provider); err != nil {
			t.Fatal(err)
		}
		other := first
		other.Amount = values.MustDecimal("999.00", 2, values.RoundingHalfUp)
		other.CanonicalDigest = other.computedDigest()
		_, err := journal.Submit(context.Background(), SubmitRequest{
			Instruction: other, Lease: settleLease(now), Now: now,
			IdempotencyKey: first.IdempotencyKey(),
		}, provider)
		if !errors.Is(err, ErrIdempotencyConflict) {
			t.Fatalf("key reuse with a different instruction must conflict, got %v", err)
		}
	})

	t.Run("expired and misbound leases are refused", func(t *testing.T) {
		journal := NewJournal()
		provider := &fakeProvider{behavior: func(PaymentInstruction) ProviderResponse {
			return ProviderResponse{Outcome: ProviderAccepted}
		}}
		expired := settleLease(now)
		expired.ExpiresAt = now.Add(-time.Second)
		if _, err := journal.Submit(context.Background(), SubmitRequest{Instruction: settleInstruction(t), Lease: expired, Now: now}, provider); err == nil {
			t.Fatal("expired lease must be refused")
		}
		misbound := settleLease(now)
		misbound.InstructionRef = "instr-other"
		if _, err := journal.Submit(context.Background(), SubmitRequest{Instruction: settleInstruction(t), Lease: misbound, Now: now}, provider); err == nil {
			t.Fatal("lease bound to another instruction must be refused")
		}
		if provider.calls.Load() != 0 {
			t.Fatal("refused submissions must never reach the provider")
		}
	})
}

// TestTodo_SETTLE_004_Race proves concurrent same-key submissions produce
// exactly one provider call and one journaled attempt.
func TestTodo_SETTLE_004_Race(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	journal := NewJournal()
	provider := &fakeProvider{behavior: func(PaymentInstruction) ProviderResponse {
		time.Sleep(5 * time.Millisecond)
		return ProviderResponse{Outcome: ProviderAccepted, ProviderRef: "prov-1"}
	}}
	done := make(chan JournalEntry, 16)
	for i := 0; i < 8; i++ {
		go func() {
			entry, err := journal.Submit(context.Background(), SubmitRequest{
				Instruction: settleInstruction(t), Lease: settleLease(now), Now: now,
			}, provider)
			if err != nil {
				t.Errorf("Submit: %v", err)
				return
			}
			done <- entry
		}()
	}
	for i := 0; i < 8; i++ {
		entry := <-done
		if entry.Outcome != OutcomeAccepted || entry.Attempt != 1 {
			t.Fatalf("every racer must see the single journaled attempt: %+v", entry)
		}
	}
	if provider.calls.Load() != 1 {
		t.Fatalf("provider calls = %d, want exactly 1", provider.calls.Load())
	}
}

// TestTodo_SETTLE_004_Integration drives submit-to-acceptance through a
// provider adapter with journaled evidence at every step.
func TestTodo_SETTLE_004_Integration(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	journal := NewJournal()
	provider := &fakeProvider{behavior: func(PaymentInstruction) ProviderResponse {
		return ProviderResponse{Outcome: ProviderAccepted, ProviderRef: "prov-adapter-7"}
	}}
	entry, err := journal.Submit(context.Background(), SubmitRequest{
		Instruction: settleInstruction(t), Lease: settleLease(now), Now: now,
	}, provider)
	if err != nil {
		t.Fatal(err)
	}
	lookedUp, ok := journal.Lookup(entry.IdempotencyKey)
	if !ok || lookedUp.ProviderRef != "prov-adapter-7" {
		t.Fatalf("journal must serve the observed provider response: %+v", lookedUp)
	}
}

// TestTodo_SETTLE_004_Fault proves provider rejection journals without
// retry storms and unknown keys cannot reconcile.
func TestTodo_SETTLE_004_Fault(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	journal := NewJournal()
	provider := &fakeProvider{behavior: func(PaymentInstruction) ProviderResponse {
		return ProviderResponse{Outcome: ProviderRejected, ProviderRef: "prov-deny-1"}
	}}
	entry, err := journal.Submit(context.Background(), SubmitRequest{
		Instruction: settleInstruction(t), Lease: settleLease(now), Now: now,
	}, provider)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Outcome != OutcomeRejected {
		t.Fatalf("provider denial must journal REJECTED, got %v", entry.Outcome)
	}
	if _, err := journal.Reconcile(ReconcileRequest{IdempotencyKey: "missing", Outcome: OutcomeAccepted, Now: now}); err == nil {
		t.Fatal("reconciling an unknown key must fail")
	}
	if _, err := journal.Reconcile(ReconcileRequest{
		IdempotencyKey: entry.IdempotencyKey, Outcome: OutcomeAccepted, ProviderRef: "prov-x", Now: now,
	}); err == nil {
		t.Fatal("settled entries must not reconcile into a new outcome")
	}
}

// TestTodo_SETTLE_004_Security proves credential-lease binding: leases are
// single-instruction, unexpired and scoped, and refused work never touches
// the provider.
func TestTodo_SETTLE_004_Security(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	journal := NewJournal()
	provider := &fakeProvider{behavior: func(PaymentInstruction) ProviderResponse {
		return ProviderResponse{Outcome: ProviderAccepted}
	}}
	scopeless := settleLease(now)
	scopeless.Scope = ""
	if _, err := journal.Submit(context.Background(), SubmitRequest{Instruction: settleInstruction(t), Lease: scopeless, Now: now}, provider); err == nil {
		t.Fatal("unscoped lease must be refused")
	}
	if provider.calls.Load() != 0 {
		t.Fatal("refused submissions must never reach the provider")
	}
}
