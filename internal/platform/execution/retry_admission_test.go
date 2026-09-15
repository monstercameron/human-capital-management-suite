package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/admissionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/coordinator"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

type retrySQLStateTestError string

func (e retrySQLStateTestError) Error() string    { return string(e) }
func (e retrySQLStateTestError) SQLState() string { return string(e) }

func TestRetryAdmissionBindsDistinctOrdinalTokensWithoutAcceptingOtherFailures(t *testing.T) {
	var store *nilRetryStore
	_, err := NewRetryAdmission(runtime.StartRequest{TenantID: uuid.New(), StartIdempotencyKey: "start:key"}, RetryBudgetSpec{Store: store, BudgetID: uuid.New(), Service: "workflow", Dependency: "postgres", OperationKind: "start", BudgetVersion: "v1", Now: time.Now, MaxResolutionAttempts: 2})
	if !errors.Is(err, ErrRetryAdmissionInvalid) {
		t.Fatalf("typed nil store err=%v", err)
	}
	if err := (RetryAdmission{}).OnRetry(context.Background(), coordinator.RetryAttempt{Attempt: 2, SQLState: "23505"}); err == nil {
		t.Fatal("non-retryable failure was admitted")
	}
	if err := (RetryAdmission{}).OnRetry(context.Background(), coordinator.RetryAttempt{Attempt: 2, SQLState: "40001"}); !errors.Is(err, ErrRetryAdmissionInvalid) {
		t.Fatalf("missing store err=%v, want binding refusal", err)
	}
}

type unknownAfterCommitStore struct {
	inner    *admissionstore.Store
	attempts []admissionstore.Attempt
	times    []time.Time
}

func (s *unknownAfterCommitStore) Consume(ctx context.Context, attempt admissionstore.Attempt, at time.Time) (admissionstore.Receipt, error) {
	s.attempts = append(s.attempts, attempt)
	s.times = append(s.times, at)
	receipt, err := s.inner.Consume(ctx, attempt, at)
	if err == nil && len(s.attempts) == 1 {
		return admissionstore.Receipt{}, admissionstore.ErrCommitUnknown
	}
	return receipt, err
}

func TestRetryAdmissionResolvesUnknownCommitWithSameDurableAttempt(t *testing.T) {
	// The deadline bounds the admission behaviour, not PostgreSQL start-up:
	// under a loaded pre-commit sweep initdb alone can take most of 15s, so
	// the clock starts once the database is up.
	db := pgtest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := time.Date(2026, 9, 8, 12, 0, 0, 123, time.UTC)
	budget := admissionstore.Budget{BudgetID: uuid.New(), TenantID: uuid.New(), Service: "workflow", Dependency: "postgres", LogicalOperationID: "start:key", OperationKind: "start", PeriodStart: now.Add(-time.Hour), PeriodEnd: now.Add(time.Hour), ExpiresAt: now.Add(time.Hour), Allowed: 1, Retryable: []admission.FailureClass{admission.FailureTransient}, Version: "v1", Owner: "test"}
	store := admissionstore.New(db.Conn)
	if err := store.CreateBudget(ctx, budget); err != nil {
		t.Fatal(err)
	}
	uncertain := &unknownAfterCommitStore{inner: store}
	clockCalls := 0
	binding, err := NewRetryAdmission(runtime.StartRequest{TenantID: budget.TenantID, StartIdempotencyKey: budget.LogicalOperationID}, RetryBudgetSpec{Store: uncertain, BudgetID: budget.BudgetID, Service: budget.Service, Dependency: budget.Dependency, OperationKind: budget.OperationKind, BudgetVersion: budget.Version, Now: func() time.Time { clockCalls++; return now }, MaxResolutionAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	attemptID, err := binding.ConsumeRetry(ctx, coordinator.RetryAttempt{Attempt: 2, SQLState: "40001"})
	if err != nil {
		t.Fatal(err)
	}
	if len(uncertain.attempts) != 2 || uncertain.attempts[0] != uncertain.attempts[1] || uncertain.attempts[0].AttemptID != attemptID || len(uncertain.times) != 2 || !uncertain.times[0].Equal(uncertain.times[1]) || clockCalls != 1 {
		t.Fatalf("attempts=%+v times=%v token=%s clockCalls=%d", uncertain.attempts, uncertain.times, attemptID, clockCalls)
	}
	if _, err := binding.ConsumeRetry(ctx, coordinator.RetryAttempt{Attempt: 2, SQLState: "40P01"}); !errors.Is(err, admissionstore.ErrExhausted) {
		t.Fatalf("fresh same-ordinal token did not compete for exhausted budget: %v", err)
	}
	if len(uncertain.attempts) != 3 || uncertain.attempts[2].AttemptID == attemptID {
		t.Fatalf("new callback reused token: %+v", uncertain.attempts)
	}
	workflowAttempts := 0
	err = coordinator.RetryClosure(ctx, coordinator.RetryOptions{
		MaxAttempts: 2,
		Admit:       func(context.Context) error { return nil },
		Sleep:       func(context.Context, time.Duration) error { return nil },
		OnRetry:     binding.OnRetry,
	}, func(context.Context) error {
		workflowAttempts++
		return retrySQLStateTestError("40001")
	})
	if !errors.Is(err, admissionstore.ErrExhausted) || workflowAttempts != 1 {
		t.Fatalf("exhausted retry err=%v workflow attempts=%d", err, workflowAttempts)
	}
}

type nilRetryStore struct{}

func (*nilRetryStore) Consume(context.Context, admissionstore.Attempt, time.Time) (admissionstore.Receipt, error) {
	panic("typed nil store used")
}

func TestNewRetryAdmissionRejectsIncompleteAndTypedNilBindings(t *testing.T) {
	request := runtime.StartRequest{TenantID: uuid.New(), StartIdempotencyKey: "start:key"}
	var store *nilRetryStore
	_, err := NewRetryAdmission(request, RetryBudgetSpec{Store: store, BudgetID: uuid.New(), Service: "workflow", Dependency: "postgres", OperationKind: "start", BudgetVersion: "v1", Now: time.Now, MaxResolutionAttempts: 2})
	if !errors.Is(err, ErrRetryAdmissionInvalid) {
		t.Fatalf("typed nil store err=%v", err)
	}
}
