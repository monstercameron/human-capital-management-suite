package execution

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/admissionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/coordinator"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// RetryAdmission binds one trusted, persisted retry budget to one logical
// request. It never provisions a budget and never derives identity from
// transport input.
type retryAdmissionStore interface {
	Consume(context.Context, admissionstore.Attempt, time.Time) (admissionstore.Receipt, error)
}

type RetryAdmission struct {
	store                 retryAdmissionStore
	budgetID, tenantID    uuid.UUID
	service, dependency   string
	logicalOperationID    string
	operationKind         string
	budgetVersion         string
	now                   func() time.Time
	maxResolutionAttempts int
}

type RetryBudgetSpec struct {
	Store                                             retryAdmissionStore
	BudgetID                                          uuid.UUID
	Service, Dependency, OperationKind, BudgetVersion string
	Now                                               func() time.Time
	MaxResolutionAttempts                             int
}

// NewRetryAdmission binds identity from the immutable StartRequest and budget
// metadata from the trusted persisted selector supplied by the caller.
func NewRetryAdmission(req runtime.StartRequest, spec RetryBudgetSpec) (RetryAdmission, error) {
	if req.TenantID == uuid.Nil || strings.TrimSpace(req.StartIdempotencyKey) == "" || nilRetryAdmissionStore(spec.Store) || spec.BudgetID == uuid.Nil || spec.Now == nil || strings.TrimSpace(spec.Service) == "" || strings.TrimSpace(spec.Dependency) == "" || strings.TrimSpace(spec.OperationKind) == "" || strings.TrimSpace(spec.BudgetVersion) == "" || spec.MaxResolutionAttempts < 1 {
		return RetryAdmission{}, ErrRetryAdmissionInvalid
	}
	return RetryAdmission{store: spec.Store, budgetID: spec.BudgetID, tenantID: req.TenantID, service: spec.Service, dependency: spec.Dependency, logicalOperationID: req.StartIdempotencyKey, operationKind: spec.OperationKind, budgetVersion: spec.BudgetVersion, now: spec.Now, maxResolutionAttempts: spec.MaxResolutionAttempts}, nil
}

var ErrRetryAdmissionInvalid = errors.New("platform execution: invalid retry admission binding")

// OnRetry consumes one persisted retry token for a classified database retry.
// Non-retryable causes pass through unchanged. ErrCommitUnknown is returned
// unchanged so its token may be resolved by a bounded durable lookup before
// any workflow retry is attempted.
func (b RetryAdmission) ConsumeRetry(ctx context.Context, retry coordinator.RetryAttempt) (ret0 uuid.UUID, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.execution.consume_retry", retry)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if retry.Attempt <= 1 || (retry.SQLState != "40001" && retry.SQLState != "40P01") {
		return uuid.Nil, fmt.Errorf("%w: retry is not a known serialization/deadlock abort", ErrRetryAdmissionInvalid)
	}
	if nilRetryAdmissionStore(b.store) || b.now == nil || b.budgetID == uuid.Nil || b.tenantID == uuid.Nil || strings.TrimSpace(b.service) == "" || strings.TrimSpace(b.dependency) == "" || strings.TrimSpace(b.logicalOperationID) == "" || strings.TrimSpace(b.operationKind) == "" || strings.TrimSpace(b.budgetVersion) == "" {
		return uuid.Nil, ErrRetryAdmissionInvalid
	}
	attemptID := uuid.New()
	attempt := admissionstore.Attempt{BudgetID: b.budgetID, AttemptID: attemptID, TenantID: b.tenantID, Service: b.service, Dependency: b.dependency, LogicalOperationID: b.logicalOperationID, OperationKind: b.operationKind, Version: b.budgetVersion, Attempt: retry.Attempt, Failure: admission.FailureTransient}
	at := b.now().UTC()
	if at.IsZero() || b.maxResolutionAttempts < 1 {
		return uuid.Nil, ErrRetryAdmissionInvalid
	}
	limit := b.maxResolutionAttempts
	for i := 0; i < limit; i++ {
		if err := ctx.Err(); err != nil {
			return attemptID, err
		}
		var receipt admissionstore.Receipt
		receipt, err := b.store.Consume(ctx, attempt, at)
		if err == nil && !retryReceiptMatches(receipt, attempt) {
			return attemptID, ErrRetryAdmissionInvalid
		}
		if !errors.Is(err, admissionstore.ErrCommitUnknown) {
			return attemptID, err
		}
	}
	return attemptID, fmt.Errorf("%w: bounded retry receipt resolution exhausted", admissionstore.ErrCommitUnknown)
}

func retryReceiptMatches(r admissionstore.Receipt, a admissionstore.Attempt) bool {
	return r.ReceiptID != uuid.Nil && r.BudgetID == a.BudgetID && r.AttemptID == a.AttemptID && r.TenantID == a.TenantID && r.Service == a.Service && r.Dependency == a.Dependency && r.LogicalOperationID == a.LogicalOperationID && r.OperationKind == a.OperationKind && r.BudgetVersion == a.Version && r.Attempt == a.Attempt && r.Failure == a.Failure && strings.TrimSpace(r.Digest) != ""
}

func nilRetryAdmissionStore(store retryAdmissionStore) bool {
	if store == nil {
		return true
	}
	v := reflect.ValueOf(store)
	return (v.Kind() == reflect.Chan || v.Kind() == reflect.Func || v.Kind() == reflect.Interface || v.Kind() == reflect.Map || v.Kind() == reflect.Pointer || v.Kind() == reflect.Slice) && v.IsNil()
}

// OnRetry is directly assignable to coordinator.RetryOptions.OnRetry. Use
// ConsumeRetry when an ErrCommitUnknown token must be retained for resolution.
func (b RetryAdmission) OnRetry(ctx context.Context, retry coordinator.RetryAttempt) error {
	_, err := b.ConsumeRetry(ctx, retry)
	return err
}
