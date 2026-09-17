package jobs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
)

var (
	// ErrAdmissionInvalid reports a batch admission request that cannot
	// start: a missing identity, an unbounded cost, an unknown priority
	// or an unusable scheduler policy.
	ErrAdmissionInvalid = errors.New("jobs: invalid batch admission request")

	// ErrUnknownJob reports work referencing a job definition this
	// scheduler never registered: unpublished or forged work never
	// starts, it is not even queued.
	ErrUnknownJob = errors.New("jobs: unknown job definition")
)

// Priority is the batch-work criticality. It maps one-to-one onto the
// admission package's P0 (highest) through P4 (lowest) vocabulary.
type Priority string

const (
	PriorityP0 Priority = "P0"
	PriorityP1 Priority = "P1"
	PriorityP2 Priority = "P2"
	PriorityP3 Priority = "P3"
	PriorityP4 Priority = "P4"
)

func (p Priority) criticality() (admission.Criticality, bool) {
	switch p {
	case PriorityP0:
		return admission.P0, true
	case PriorityP1:
		return admission.P1, true
	case PriorityP2:
		return admission.P2, true
	case PriorityP3:
		return admission.P3, true
	case PriorityP4:
		return admission.P4, true
	default:
		return "", false
	}
}

// SchedulerPolicy bounds one batch scheduler. CellCapacity is the total
// cost the cell admits; ReservedP0 is the slice of it only P0 work can
// consume, so a low-priority flood can never starve critical work.
// TenantLimit caps admitted plus queued cost per tenant. RetryAllowance
// provisions the per-operation retry budget recorded as evidence.
type SchedulerPolicy struct {
	CellID         string
	CellCapacity   int
	ReservedP0     int
	TenantLimit    int
	RetryAllowance int
	QuotaVersion   string
}

// AdmissionRequest is one batch-work admission. DefinitionDigest binds
// the published JOB-001 definition revision the run would execute;
// EstimatedCost is the positive, bounded cost the cell accounts.
type AdmissionRequest struct {
	TenantID         uuid.UUID
	JobID            string
	DefinitionDigest string
	Priority         Priority
	EstimatedCost    int
	OperationID      string
}

// AdmissionReceipt is the sealed admission record: the deterministic
// ADMIT|QUEUE|DEFER|DEGRADE|REJECT decision plus the retry-budget
// evidence for the operation.
type AdmissionReceipt struct {
	Sequence       uint64
	TenantID       uuid.UUID
	JobID          string
	Priority       Priority
	Outcome        admission.Outcome
	Reason         string
	DecisionID     string
	BudgetID       string
	BudgetAllowed  int
	BudgetConsumed int
	EvidenceDigest string
}

// Scheduler admits batch work by priority, quota and cost. It composes
// the admission package's deterministic Decide policy with durable
// per-operation retry budgets, keeping the cell and tenant counters
// under one mutex. It is safe for concurrent use.
type Scheduler struct {
	mu          sync.Mutex
	policy      SchedulerPolicy
	definitions map[string]string
	budgets     *admission.Provisioner
	cellUsed    int
	tenantUsed  map[string]int
	pending     map[string]int
	ledger      []AdmissionReceipt
	sequence    uint64
}

// NewScheduler validates one scheduler policy.
func NewScheduler(policy SchedulerPolicy) (*Scheduler, error) {
	if strings.TrimSpace(policy.CellID) == "" || policy.CellCapacity < 1 ||
		policy.ReservedP0 < 0 || policy.ReservedP0 > policy.CellCapacity ||
		policy.TenantLimit < 1 || policy.RetryAllowance < 0 ||
		strings.TrimSpace(policy.QuotaVersion) == "" {
		return nil, fmt.Errorf("%w: scheduler policy needs a cell, positive capacity and limits", ErrAdmissionInvalid)
	}
	return &Scheduler{
		policy:      policy,
		definitions: make(map[string]string),
		budgets:     admission.NewProvisioner(),
		tenantUsed:  make(map[string]int),
		pending:     make(map[string]int),
	}, nil
}

// Register binds one published job definition digest for a tenant. Only
// registered work can be admitted; anything else is unknown.
func (s *Scheduler) Register(tenantID uuid.UUID, jobID, definitionDigest string) error {
	if tenantID == uuid.Nil {
		return fmt.Errorf("%w: tenant is required", ErrAdmissionInvalid)
	}
	if !validIdentifier(jobID) {
		return fmt.Errorf("%w: job id names a job with an unpadded identifier", ErrAdmissionInvalid)
	}
	if !isHex64(definitionDigest) {
		return fmt.Errorf("%w: definition digest is not 64 hex characters", ErrAdmissionInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.definitions[tenantID.String()+"/"+jobID] = definitionDigest
	return nil
}

func sealReceipt(receipt AdmissionReceipt) AdmissionReceipt {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		fmt.Sprintf("%d", receipt.Sequence),
		receipt.TenantID.String(), receipt.JobID, string(receipt.Priority),
		string(receipt.Outcome), receipt.DecisionID, receipt.BudgetID,
	}, "\x00")))
	receipt.EvidenceDigest = "sha256:" + hex.EncodeToString(sum[:])
	return receipt
}

// Admit decides one batch-work request deterministically. Invalid or
// unknown work is refused with an error and never starts; work the cell
// cannot take now receives QUEUE, DEFER, DEGRADE or REJECT with a sealed
// receipt. P0 work draws on the reserved slice, so low-priority floods
// preserve critical capacity.
func (s *Scheduler) Admit(request AdmissionRequest) (AdmissionReceipt, error) {
	criticality, ok := request.Priority.criticality()
	if !ok {
		return AdmissionReceipt{}, fmt.Errorf("%w: priority %q is not P0-P4", ErrAdmissionInvalid, request.Priority)
	}
	if request.TenantID == uuid.Nil {
		return AdmissionReceipt{}, fmt.Errorf("%w: tenant is required", ErrAdmissionInvalid)
	}
	if !validIdentifier(request.JobID) {
		return AdmissionReceipt{}, fmt.Errorf("%w: job id names a job with an unpadded identifier", ErrAdmissionInvalid)
	}
	if !isHex64(request.DefinitionDigest) {
		return AdmissionReceipt{}, fmt.Errorf("%w: definition digest is not 64 hex characters", ErrAdmissionInvalid)
	}
	if request.EstimatedCost < 1 {
		return AdmissionReceipt{}, fmt.Errorf("%w: estimated cost is unknown or unbounded", ErrAdmissionInvalid)
	}
	if strings.TrimSpace(request.OperationID) == "" || request.OperationID != strings.TrimSpace(request.OperationID) {
		return AdmissionReceipt{}, fmt.Errorf("%w: operation id is required exact, without padding", ErrAdmissionInvalid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	published, known := s.definitions[request.TenantID.String()+"/"+request.JobID]
	if !known || published != request.DefinitionDigest {
		return AdmissionReceipt{}, fmt.Errorf("%w: %s for tenant %s", ErrUnknownJob, request.JobID, request.TenantID.String())
	}
	budget, err := s.budgets.Provision(admission.ProvisionSpec{
		TenantID: request.TenantID.String(), Service: "jobs", Dependency: "batch-cell",
		LogicalOperationID: request.OperationID, OperationKind: "batch",
		Allowed:   s.policy.RetryAllowance,
		Retryable: []admission.FailureClass{admission.FailureTransient, admission.FailureTimeout, admission.FailureUnavailable, admission.FailureThrottled},
		Version:   "v1",
	})
	if err != nil {
		return AdmissionReceipt{}, fmt.Errorf("jobs: provision retry budget: %w", err)
	}
	tenant := request.TenantID.String()
	// The snapshot carries remaining cell capacity. The P0 reservation is
	// clamped to what remains: once the free slice is exhausted every free
	// unit is effectively reserved, which is exactly what preserves
	// critical capacity under a low-priority flood.
	remaining := s.policy.CellCapacity - s.cellUsed
	decision := admission.Decide(
		admission.Request{TenantID: tenant, CellID: s.policy.CellID, PlacementEpoch: 1, Criticality: criticality, EstimatedCost: request.EstimatedCost, RetryBudgetID: budget.ID},
		admission.Snapshot{TenantID: tenant, CellID: s.policy.CellID, PlacementEpoch: 1,
			Quota:          admission.Quota{Known: true, Version: s.policy.QuotaVersion, Limit: s.policy.TenantLimit, Consumed: s.tenantUsed[tenant], Pending: s.pending[tenant]},
			Capacity:       remaining,
			ReservedP0:     min(s.policy.ReservedP0, remaining),
			RetryRemaining: budget.Allowed - budget.Consumed + budget.Refunded},
		admission.Policy{},
	)
	if err := decision.Validate(); err != nil {
		return AdmissionReceipt{}, fmt.Errorf("jobs: admission decision: %w", err)
	}
	switch decision.Outcome {
	case admission.Admit:
		s.cellUsed += request.EstimatedCost
		s.tenantUsed[tenant] += request.EstimatedCost
	case admission.Queue:
		s.pending[tenant] += request.EstimatedCost
	}
	s.sequence++
	receipt := sealReceipt(AdmissionReceipt{
		Sequence: s.sequence, TenantID: request.TenantID, JobID: request.JobID,
		Priority: request.Priority, Outcome: decision.Outcome, Reason: decision.Reason,
		DecisionID: decision.DecisionID, BudgetID: budget.ID,
		BudgetAllowed: budget.Allowed, BudgetConsumed: budget.Consumed,
	})
	s.ledger = append(s.ledger, receipt)
	return receipt, nil
}

// Ledger returns every admission receipt in sequence.
func (s *Scheduler) Ledger() []AdmissionReceipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AdmissionReceipt(nil), s.ledger...)
}

// CellUsed reports the admitted cost currently held by the cell.
func (s *Scheduler) CellUsed() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cellUsed
}
