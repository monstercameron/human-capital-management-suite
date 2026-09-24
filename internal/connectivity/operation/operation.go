// Package operation owns the governed outbound connector-operation kernel.
//
// The package is deliberately storage-neutral. MemoryJournal is a complete
// semantic implementation for the Promotion pilot and a conformance double
// for the PostgreSQL rows described by the migrations. It never calls a
// provider while planning, leasing, revalidating, observing, or redriving.
package operation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Version is the operation-kernel contract version (ARCH-GO-009).
func Version() int { return 1 }

// State is the durable lifecycle of one connector operation.
type State string

const (
	StatePlanned          State = "PLANNED"
	StateQueued           State = "QUEUED"
	StateLeased           State = "LEASED"
	StateSending          State = "SENDING"
	StateSent             State = "SENT"
	StateProviderAccepted State = "PROVIDER_ACCEPTED"
	StateObserving        State = "OBSERVING"
	StateReconciled       State = "RECONCILED"
	StateFailed           State = "FAILED"
	StateRetryable        State = "RETRYABLE"
	StateDeadLetter       State = "DEAD_LETTER"
	StateAmbiguous        State = "AMBIGUOUS"
	StateRepairRequired   State = "REPAIR_REQUIRED"
	StateRejected         State = "REJECTED"
)

// OrderingClass controls whether an operation participates in resource
// serialization.
type OrderingClass string

const (
	OrderingStrict              OrderingClass = "STRICT"
	OrderingCommutative         OrderingClass = "COMMUTATIVE"
	OrderingIndependent         OrderingClass = "INDEPENDENT"
	OrderingProviderConditional OrderingClass = "PROVIDER_CONDITIONAL"
)

// ResponseClass is the normalized provider outcome. Provider acceptance is
// not business completion; completion requires an observation.
type ResponseClass string

const (
	ResponseSuccess   ResponseClass = "SUCCESS"
	ResponseFailure   ResponseClass = "FAILURE"
	ResponsePartial   ResponseClass = "PARTIAL"
	ResponseUnknown   ResponseClass = "UNKNOWN"
	ResponseAmbiguous ResponseClass = "AMBIGUOUS"
	ResponsePending   ResponseClass = "PENDING"
)

// RetryDisposition is the next governed action after an attempt.
type RetryDisposition string

const (
	RetryNone              RetryDisposition = "NONE"
	Retry                  RetryDisposition = "RETRY"
	RetryDeadLetter        RetryDisposition = "DEAD_LETTER"
	RetryObservationNeeded RetryDisposition = "OBSERVATION_REQUIRED"
	RetryRepairNeeded      RetryDisposition = "REPAIR_REQUIRED"
)

// ObservationRequirement is intentionally typed. A timeout cannot be
// resolved by a caller merely asserting success or by blindly resending.
type ObservationRequirement string

const (
	ObservationBySemanticIdentity ObservationRequirement = "SEMANTIC_IDENTITY"
)

// ObservationVerdict is the provider read-back result for an operation.
type ObservationVerdict string

const (
	ObservationApplied    ObservationVerdict = "APPLIED"
	ObservationNotApplied ObservationVerdict = "NOT_APPLIED"
	ObservationConflict   ObservationVerdict = "CONFLICT"
	ObservationUnknown    ObservationVerdict = "UNKNOWN"
)

var (
	ErrInvalid                 = errors.New("operation: invalid operation")
	ErrNotFound                = errors.New("operation: operation not found")
	ErrImmutable               = errors.New("operation: operation identity is immutable")
	ErrDuplicate               = errors.New("operation: duplicate semantic operation")
	ErrInvalidTransition       = errors.New("operation: invalid lifecycle transition")
	ErrLeaseRequired           = errors.New("operation: valid dispatch lease is required")
	ErrLeaseExpired            = errors.New("operation: dispatch lease expired")
	ErrLeaseFenced             = errors.New("operation: dispatch lease fence refused")
	ErrRevalidationRequired    = errors.New("operation: lease-time revalidation is required")
	ErrRevalidationBlocked     = errors.New("operation: lease-time revalidation blocked dispatch")
	ErrCausalBlocked           = errors.New("operation: causal predecessor or resource sequence unresolved")
	ErrProviderRequired        = errors.New("operation: provider writer is required")
	ErrObservationRequired     = errors.New("operation: typed external observation is required")
	ErrRedriveNotAllowed       = errors.New("operation: operation is not redrivable")
	ErrRedriveApprovalRequired = errors.New("operation: redrive approval is required")
	ErrRepairPlanRequired      = errors.New("operation: material redrive change requires a repair plan")
)

// Operation is the journal row. Payload bytes are optional test/fixture
// material; explanations and errors never render them.
type Operation struct {
	OperationID      uuid.UUID
	TenantID         string
	ConnectionID     string
	ConnectorVersion string

	BusinessTransactionID string
	WorkflowInstanceID    string
	RepairPlanID          string

	SemanticOperation   string
	Direction           string
	Criticality         string
	ExternalResourceKey string
	OrderingClass       OrderingClass
	CausalPredecessorID uuid.UUID
	ResourceSequence    uint64

	ExpectedExternalVersion    string
	SourceAuthorityDecisionRef string
	AuthorityPolicyFingerprint string
	WriterFenceEpoch           uint64
	AuthorityCutoverWatermark  string

	CanonicalInputRef     string
	CanonicalInputDigest  string
	MappingProfileVersion string
	MappedPayloadRef      string
	MappedPayloadDigest   string
	MappedPayload         []byte

	Classification string
	Purpose        string
	DestinationRef string
	CredentialRef  string

	IdempotencyKey         string
	ExternalIdempotencyKey string
	ObservationRequirement ObservationRequirement

	DeadlineAt           time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
	State                State
	CompletionState      string
	ResponseClass        ResponseClass
	ObservationID        uuid.UUID
	ReconciliationStatus string

	FenceToken   uint64
	Attempts     []Attempt
	RedriveCount int
}

// PlanRequest is the complete pre-dispatch journal input. Plan persists this
// row before Queue or Lease can succeed.
type PlanRequest struct {
	OperationID      uuid.UUID
	TenantID         string
	ConnectionID     string
	ConnectorVersion string

	BusinessTransactionID string
	WorkflowInstanceID    string
	RepairPlanID          string
	SemanticOperation     string
	Direction             string
	Criticality           string
	ExternalResourceKey   string
	OrderingClass         OrderingClass
	CausalPredecessorID   uuid.UUID
	ResourceSequence      uint64

	ExpectedExternalVersion    string
	SourceAuthorityDecisionRef string
	AuthorityPolicyFingerprint string
	WriterFenceEpoch           uint64
	AuthorityCutoverWatermark  string
	CanonicalInputRef          string
	CanonicalInputDigest       string
	MappingProfileVersion      string
	MappedPayloadRef           string
	MappedPayloadDigest        string
	MappedPayload              []byte
	Classification             string
	Purpose                    string
	DestinationRef             string
	CredentialRef              string
	IdempotencyKey             string
	ExternalIdempotencyKey     string
	ObservationRequirement     ObservationRequirement
	CreatedAt                  time.Time
	DeadlineAt                 time.Time
}

// Attempt is an immutable normalized record of one provider call.
type Attempt struct {
	AttemptID         uuid.UUID
	OperationID       uuid.UUID
	AttemptNumber     int
	RequestDigest     string
	ResponseDigest    string
	ProviderRequestID string
	ProviderResult    ResponseClass
	RetryDisposition  RetryDisposition
	FenceToken        uint64
	AttemptedAt       time.Time
	ReceivedAt        time.Time
}

// Lease is a fenced right to make one provider call.
type Lease struct {
	TenantID    string
	OperationID uuid.UUID
	Token       uuid.UUID
	FenceToken  uint64
	WorkerID    string
	ExpiresAt   time.Time
}

// Revalidation is the only evidence Lease accepts for a dispatch decision.
// PlanDigest and CurrentPlanDigest bind the evidence to the prepared plan.
type Revalidation struct {
	OperationID       uuid.UUID
	Confirmed         bool
	Requirement       string
	ChangedInputs     []string
	PlanDigest        string
	CurrentPlanDigest string
	Explanation       string
}

// RevalidateFunc is called immediately before the lease is granted.
type RevalidateFunc func(Operation) Revalidation

// LeaseRequest asks the journal to revalidate and lease one queued operation.
type LeaseRequest struct {
	TenantID    string
	OperationID uuid.UUID
	WorkerID    string
	At          time.Time
	Duration    time.Duration
	Revalidate  RevalidateFunc
}

// WriteRequest is the provider-facing request. The semantic key is stable
// across retries and redrives of the same logical effect.
type WriteRequest struct {
	OperationID             uuid.UUID
	SemanticOperation       string
	ExternalResourceKey     string
	ExpectedExternalVersion string
	IdempotencyKey          string
	Payload                 []byte
}

// WriteResponse is a normalized provider response.
type WriteResponse struct {
	Result            ResponseClass
	ProviderRequestID string
	ExternalObjectRef string
	ExternalVersion   string
	ResponseDigest    string
}

// Writer is the narrow external mutation port. Authority and retry policy
// stay in this package; a provider client only transports this request.
type Writer interface {
	Write(context.Context, WriteRequest) (WriteResponse, error)
}

// ProviderError preserves whether a transport failure may have happened after
// the provider received the request.
type ProviderError struct {
	Result      ResponseClass
	MayHaveSent bool
	Cause       error
}

func (e *ProviderError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause == nil {
		return string(e.Result)
	}
	return string(e.Result) + ": " + e.Cause.Error()
}

func (e *ProviderError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// DispatchResult explains whether a provider call occurred and what durable
// state was recorded.
type DispatchResult struct {
	Operation           Operation
	Attempt             Attempt
	ProviderCall        bool
	ObservationRequired bool
}

// Observation is a typed provider read-back. It is the only input that can
// move an accepted or ambiguous operation to business completion.
type Observation struct {
	TenantID            string
	ObservationID       uuid.UUID
	OperationID         uuid.UUID
	ExternalResourceKey string
	Verdict             ObservationVerdict
	ExternalObjectRef   string
	ExternalVersion     string
	ObservedDigest      string
	ObservedAt          time.Time
	Authority           string
}

// ProviderReadBack is unnormalized, provider-specific evidence gathered when
// checking whether a semantic write actually took effect. It carries no
// verdict: NormalizeObservation is the only place a [ObservationVerdict] is
// computed, from identity and content comparison against what this package
// actually dispatched, never from a caller's own assertion.
type ProviderReadBack struct {
	ExternalResourceKey string
	Found               bool
	Ambiguous           bool
	ExternalObjectRef   string
	ExternalVersion     string
	ObservedDigest      string
}

// NormalizeObservation turns one provider-specific read-back into the typed
// Observation RecordObservation accepts. A caller cannot manufacture an
// APPLIED verdict by construction: this is the single place a verdict is
// computed, by comparing the read-back's resource identity and content digest
// against the operation this package dispatched.
func NormalizeObservation(op Operation, raw ProviderReadBack, observationID uuid.UUID, observedAt time.Time, authority string) (Observation, error) {
	if observationID == uuid.Nil {
		return Observation{}, fmt.Errorf("%w: observation id is required", ErrObservationRequired)
	}
	if observedAt.IsZero() {
		return Observation{}, fmt.Errorf("%w: observation time is required", ErrObservationRequired)
	}
	if strings.TrimSpace(raw.ExternalResourceKey) == "" || raw.ExternalResourceKey != op.ExternalResourceKey {
		return Observation{}, fmt.Errorf("%w: read-back resource does not match operation", ErrObservationRequired)
	}
	var verdict ObservationVerdict
	switch {
	case raw.Ambiguous:
		verdict = ObservationUnknown
	case !raw.Found:
		verdict = ObservationNotApplied
	case attemptDigestMatches(op, raw.ObservedDigest):
		verdict = ObservationApplied
	default:
		verdict = ObservationConflict
	}
	return Observation{
		TenantID:            op.TenantID,
		ObservationID:       observationID,
		OperationID:         op.OperationID,
		ExternalResourceKey: raw.ExternalResourceKey,
		Verdict:             verdict,
		ExternalObjectRef:   raw.ExternalObjectRef,
		ExternalVersion:     raw.ExternalVersion,
		ObservedDigest:      raw.ObservedDigest,
		ObservedAt:          observedAt.UTC(),
		Authority:           authority,
	}, nil
}

// attemptDigestMatches reports whether a read-back's content digest matches
// any attempt this package sent, so a re-read of the provider's own record
// can be recognized as the effect of our write rather than someone else's.
func attemptDigestMatches(op Operation, digest string) bool {
	if strings.TrimSpace(digest) == "" {
		return false
	}
	for _, attempt := range op.Attempts {
		if attempt.RequestDigest == digest {
			return true
		}
	}
	return false
}

// RedriveRequest is the current-state comparison used before a failed
// operation is put back in the queue. It never changes the original payload.
type RedriveRequest struct {
	TenantID                          string
	OperationID                       uuid.UUID
	At                                time.Time
	CurrentMappingProfileVersion      string
	CurrentMappedPayloadDigest        string
	CurrentAuthorityPolicyFingerprint string
	CurrentWriterFenceEpoch           uint64
	CurrentCredentialRef              string
	Approved                          bool
	RepairPlanID                      string
	ActorRef                          string
}

// RedrivePreview is safe to show before approval and contains no payload.
type RedrivePreview struct {
	OperationID      uuid.UUID
	Compatible       bool
	RequiresApproval bool
	MaterialChanges  []string
	Explanation      string
}

// RedriveRecord proves the failed operation, actor and approval remain in one
// lineage. It is append-only in the storage design.
type RedriveRecord struct {
	RedriveID       uuid.UUID
	OperationID     uuid.UUID
	ActorRef        string
	RepairPlanID    string
	At              time.Time
	MaterialChanges []string
}

// MemoryJournal is a concurrency-safe semantic journal.
type MemoryJournal struct {
	mu              sync.RWMutex
	operations      map[uuid.UUID]Operation
	operationSeqs   map[uuid.UUID]uint64
	redrives        map[uuid.UUID][]RedriveRecord
	activeLeases    map[uuid.UUID]Lease
	events          []JournalEvent
	journalSequence uint64
	journalDigest   string
	now             func() time.Time
}

// NewMemoryJournal constructs an empty journal.
func NewMemoryJournal(now func() time.Time) *MemoryJournal {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemoryJournal{operations: make(map[uuid.UUID]Operation), operationSeqs: make(map[uuid.UUID]uint64), redrives: make(map[uuid.UUID][]RedriveRecord), activeLeases: make(map[uuid.UUID]Lease), now: now}
}

// SeedOperationSequence advances the next transition sequence for a restored
// operation. Durable adapters call it after replaying the original plan into
// a fresh semantic journal so synthetic restore events never collide with
// transitions already stored by a prior process.
func (j *MemoryJournal) SeedOperationSequence(id uuid.UUID, sequence uint64) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, ok := j.operations[id]; !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if sequence > j.operationSeqs[id] {
		j.operationSeqs[id] = sequence
	}
	return nil
}

// Plan durably records a complete operation before it can be queued or leased.
func (j *MemoryJournal) Plan(ctx context.Context, req PlanRequest) (Operation, error) {
	if err := contextError(ctx); err != nil {
		return Operation{}, err
	}
	created := req.CreatedAt.UTC()
	if created.IsZero() {
		created = j.now().UTC()
	}
	if req.DeadlineAt.IsZero() {
		return Operation{}, fmt.Errorf("%w: deadline is required", ErrInvalid)
	}
	if !req.DeadlineAt.After(created) {
		return Operation{}, fmt.Errorf("%w: deadline must be after creation", ErrInvalid)
	}
	id := req.OperationID
	if id == uuid.Nil {
		id = uuid.New()
	}
	op := Operation{
		OperationID: id, TenantID: req.TenantID, ConnectionID: req.ConnectionID, ConnectorVersion: req.ConnectorVersion,
		BusinessTransactionID: req.BusinessTransactionID, WorkflowInstanceID: req.WorkflowInstanceID, RepairPlanID: req.RepairPlanID,
		SemanticOperation: req.SemanticOperation, Direction: req.Direction, Criticality: req.Criticality,
		ExternalResourceKey: req.ExternalResourceKey, OrderingClass: req.OrderingClass, CausalPredecessorID: req.CausalPredecessorID,
		ResourceSequence: req.ResourceSequence, ExpectedExternalVersion: req.ExpectedExternalVersion,
		SourceAuthorityDecisionRef: req.SourceAuthorityDecisionRef, AuthorityPolicyFingerprint: req.AuthorityPolicyFingerprint,
		WriterFenceEpoch: req.WriterFenceEpoch, AuthorityCutoverWatermark: req.AuthorityCutoverWatermark,
		CanonicalInputRef: req.CanonicalInputRef, CanonicalInputDigest: req.CanonicalInputDigest,
		MappingProfileVersion: req.MappingProfileVersion, MappedPayloadRef: req.MappedPayloadRef, MappedPayloadDigest: req.MappedPayloadDigest,
		MappedPayload: append([]byte(nil), req.MappedPayload...), Classification: req.Classification, Purpose: req.Purpose,
		DestinationRef: req.DestinationRef, CredentialRef: req.CredentialRef, IdempotencyKey: req.IdempotencyKey,
		ExternalIdempotencyKey: req.ExternalIdempotencyKey, ObservationRequirement: req.ObservationRequirement,
		DeadlineAt: req.DeadlineAt.UTC(), CreatedAt: created, UpdatedAt: created, State: StatePlanned,
		CompletionState: "PENDING", ResponseClass: ResponsePending, ReconciliationStatus: "PENDING",
	}
	if op.ExternalIdempotencyKey == "" {
		op.ExternalIdempotencyKey = op.IdempotencyKey
	}
	if op.ObservationRequirement == "" {
		op.ObservationRequirement = ObservationBySemanticIdentity
	}
	if err := op.validate(); err != nil {
		return Operation{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, ok := j.operations[id]; ok {
		return Operation{}, fmt.Errorf("%w: operation id %s", ErrDuplicate, id)
	}
	for _, existing := range j.operations {
		if existing.TenantID == op.TenantID && existing.ConnectionID == op.ConnectionID && existing.IdempotencyKey == op.IdempotencyKey {
			return Operation{}, fmt.Errorf("%w: idempotency key %q", ErrDuplicate, op.IdempotencyKey)
		}
	}
	j.operations[id] = cloneOperation(op)
	j.appendJournalLocked(op, State(""), StatePlanned, "PLANNED", uuid.Nil, 0, "", "", created)
	return cloneOperation(op), nil
}

// Queue makes a planned operation eligible for a dispatch lease.
func (j *MemoryJournal) Queue(ctx context.Context, tenant string, id uuid.UUID) (Operation, error) {
	if err := contextError(ctx); err != nil {
		return Operation{}, err
	}
	if strings.TrimSpace(tenant) == "" {
		return Operation{}, fmt.Errorf("%w: tenant is required", ErrInvalid)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	op, ok := j.operations[id]
	if !ok || op.TenantID != tenant {
		return Operation{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if op.State != StatePlanned && op.State != StateRetryable && op.State != StateFailed {
		return Operation{}, fmt.Errorf("%w: %s -> QUEUED", ErrInvalidTransition, op.State)
	}
	from := op.State
	op.State, op.UpdatedAt = StateQueued, j.now().UTC()
	j.operations[id] = cloneOperation(op)
	j.appendJournalLocked(op, from, StateQueued, "QUEUED", uuid.Nil, op.FenceToken, "", "", op.UpdatedAt)
	return cloneOperation(op), nil
}

// Get returns a defensive copy of a journal row.
func (j *MemoryJournal) Get(ctx context.Context, tenant string, id uuid.UUID) (Operation, error) {
	if err := contextError(ctx); err != nil {
		return Operation{}, err
	}
	if strings.TrimSpace(tenant) == "" {
		return Operation{}, fmt.Errorf("%w: tenant is required", ErrInvalid)
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	op, ok := j.operations[id]
	if !ok || op.TenantID != tenant {
		return Operation{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return cloneOperation(op), nil
}

// List returns rows in stable resource/sequence/id order.
func (j *MemoryJournal) List(ctx context.Context, tenant string) ([]Operation, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(tenant) == "" {
		return nil, fmt.Errorf("%w: tenant is required", ErrInvalid)
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	out := make([]Operation, 0)
	for _, op := range j.operations {
		if op.TenantID == tenant {
			out = append(out, cloneOperation(op))
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].ExternalResourceKey != out[b].ExternalResourceKey {
			return out[a].ExternalResourceKey < out[b].ExternalResourceKey
		}
		if out[a].ResourceSequence != out[b].ResourceSequence {
			return out[a].ResourceSequence < out[b].ResourceSequence
		}
		return out[a].OperationID.String() < out[b].OperationID.String()
	})
	return out, nil
}

// Lease revalidates at the dispatch boundary and grants a fenced lease only
// when the current governance evidence confirms the exact prepared plan.
func (j *MemoryJournal) Lease(ctx context.Context, req LeaseRequest) (Lease, error) {
	if err := contextError(ctx); err != nil {
		return Lease{}, err
	}
	if req.Revalidate == nil {
		return Lease{}, ErrRevalidationRequired
	}
	if strings.TrimSpace(req.TenantID) == "" {
		return Lease{}, fmt.Errorf("%w: tenant is required", ErrInvalid)
	}
	if req.WorkerID == "" {
		return Lease{}, fmt.Errorf("%w: worker id is required", ErrInvalid)
	}
	at := req.At.UTC()
	if at.IsZero() {
		at = j.now().UTC()
	}
	if req.Duration <= 0 {
		return Lease{}, fmt.Errorf("%w: lease duration must be positive", ErrInvalid)
	}
	j.mu.Lock()
	current, present := j.operations[req.OperationID]
	if !present || current.TenantID != req.TenantID {
		j.mu.Unlock()
		return Lease{}, fmt.Errorf("%w: %s", ErrNotFound, req.OperationID)
	}
	if active, exists := j.activeLeases[req.OperationID]; exists && !at.Before(active.ExpiresAt) {
		if current.State == StateLeased {
			from := current.State
			current.State, current.UpdatedAt = StateQueued, at
			j.operations[req.OperationID] = cloneOperation(current)
			j.appendJournalLocked(current, from, StateQueued, "LEASE_EXPIRED", uuid.Nil, active.FenceToken, "", "", at)
		}
		delete(j.activeLeases, req.OperationID)
	}
	j.mu.Unlock()
	j.mu.RLock()
	op, ok := j.operations[req.OperationID]
	if !ok || op.TenantID != req.TenantID {
		j.mu.RUnlock()
		return Lease{}, fmt.Errorf("%w: %s", ErrNotFound, req.OperationID)
	}
	op = cloneOperation(op)
	j.mu.RUnlock()
	if op.State == StateLeased {
		return Lease{}, ErrLeaseFenced
	}
	if op.State != StateQueued && op.State != StateRetryable && op.State != StateFailed {
		return Lease{}, fmt.Errorf("%w: %s is not queued", ErrInvalidTransition, op.State)
	}
	if !at.Before(op.DeadlineAt) {
		return Lease{}, fmt.Errorf("%w: operation deadline passed", ErrInvalid)
	}
	check := req.Revalidate(op)
	if check.OperationID != uuid.Nil && check.OperationID != op.OperationID {
		return Lease{}, fmt.Errorf("%w: evidence names %s, want %s", ErrRevalidationBlocked, check.OperationID, op.OperationID)
	}
	if !check.Confirmed || check.PlanDigest == "" || check.CurrentPlanDigest == "" || check.PlanDigest != check.CurrentPlanDigest {
		j.mu.Lock()
		current := j.operations[op.OperationID]
		from := current.State
		current.State = StateRepairRequired
		if check.Requirement == "BLOCK" {
			current.State = StateRejected
		}
		current.UpdatedAt = at
		j.operations[op.OperationID] = cloneOperation(current)
		j.appendJournalLocked(current, from, current.State, "REVALIDATION_BLOCKED", uuid.Nil, current.FenceToken, "", "", at)
		j.mu.Unlock()
		return Lease{}, fmt.Errorf("%w: %s", ErrRevalidationBlocked, check.Explanation)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if active, exists := j.activeLeases[op.OperationID]; exists {
		if at.Before(active.ExpiresAt) {
			return Lease{}, ErrLeaseFenced
		}
		if current, present := j.operations[op.OperationID]; present && current.State == StateLeased {
			from := current.State
			current.State, current.UpdatedAt = StateQueued, at
			j.operations[op.OperationID] = cloneOperation(current)
			j.appendJournalLocked(current, from, StateQueued, "LEASE_EXPIRED", uuid.Nil, active.FenceToken, "", "", at)
		}
		delete(j.activeLeases, op.OperationID)
	}
	current, ok = j.operations[op.OperationID]
	if !ok {
		return Lease{}, fmt.Errorf("%w: %s", ErrNotFound, op.OperationID)
	}
	if current.State != StateQueued && current.State != StateRetryable && current.State != StateFailed {
		return Lease{}, fmt.Errorf("%w: state changed to %s", ErrLeaseFenced, current.State)
	}
	if !j.causalReadyLocked(current) {
		return Lease{}, fmt.Errorf("%w: resource %s sequence %d", ErrCausalBlocked, current.ExternalResourceKey, current.ResourceSequence)
	}
	current.State = StateLeased
	current.FenceToken++
	current.UpdatedAt = at
	j.operations[current.OperationID] = cloneOperation(current)
	granted := Lease{TenantID: current.TenantID, OperationID: current.OperationID, Token: uuid.New(), FenceToken: current.FenceToken, WorkerID: req.WorkerID, ExpiresAt: at.Add(req.Duration)}
	j.activeLeases[current.OperationID] = granted
	j.appendJournalLocked(current, StateQueued, StateLeased, "LEASED", uuid.Nil, current.FenceToken, "", "", at)
	return granted, nil
}

func (j *MemoryJournal) causalReadyLocked(current Operation) bool {
	if current.OrderingClass != OrderingStrict && current.OrderingClass != OrderingProviderConditional {
		return true
	}
	if current.CausalPredecessorID != uuid.Nil {
		pred, ok := j.operations[current.CausalPredecessorID]
		if !ok || pred.TenantID != current.TenantID || pred.ConnectionID != current.ConnectionID || pred.ExternalResourceKey != current.ExternalResourceKey || pred.State != StateReconciled {
			return false
		}
	}
	for _, prior := range j.operations {
		if prior.OperationID == current.OperationID || prior.TenantID != current.TenantID || prior.ConnectionID != current.ConnectionID || prior.ExternalResourceKey != current.ExternalResourceKey || prior.ResourceSequence == 0 || current.ResourceSequence == 0 || prior.ResourceSequence >= current.ResourceSequence {
			continue
		}
		if prior.State != StateReconciled {
			return false
		}
	}
	return true
}

// Dispatch makes at most one provider call for the supplied fenced lease.
func (j *MemoryJournal) Dispatch(ctx context.Context, lease Lease, writer Writer) (DispatchResult, error) {
	if err := contextError(ctx); err != nil {
		return DispatchResult{}, err
	}
	if writer == nil {
		return DispatchResult{}, ErrProviderRequired
	}
	now := j.now().UTC()
	j.mu.Lock()
	op, ok := j.operations[lease.OperationID]
	if !ok {
		j.mu.Unlock()
		return DispatchResult{}, fmt.Errorf("%w: %s", ErrNotFound, lease.OperationID)
	}
	if lease.TenantID == "" || lease.TenantID != op.TenantID {
		j.mu.Unlock()
		return DispatchResult{}, ErrLeaseFenced
	}
	if op.State == StateProviderAccepted || op.State == StateObserving || op.State == StateReconciled {
		result := DispatchResult{Operation: cloneOperation(op), ProviderCall: false, ObservationRequired: op.State != StateReconciled}
		j.mu.Unlock()
		return result, nil
	}
	if op.State != StateLeased {
		j.mu.Unlock()
		return DispatchResult{}, ErrLeaseRequired
	}
	active, activeExists := j.activeLeases[lease.OperationID]
	if lease.Token == uuid.Nil || (activeExists && active.Token != lease.Token) || lease.FenceToken != op.FenceToken || lease.ExpiresAt.IsZero() || !now.Before(lease.ExpiresAt) {
		j.mu.Unlock()
		return DispatchResult{}, ErrLeaseExpired
	}
	op.State = StateSending
	op.UpdatedAt = now
	j.operations[op.OperationID] = cloneOperation(op)
	j.appendJournalLocked(op, StateLeased, StateSending, "SENDING", uuid.Nil, lease.FenceToken, "", op.MappedPayloadDigest, now)
	j.mu.Unlock()
	response, callErr := writer.Write(ctx, WriteRequest{OperationID: op.OperationID, SemanticOperation: op.SemanticOperation, ExternalResourceKey: op.ExternalResourceKey, ExpectedExternalVersion: op.ExpectedExternalVersion, IdempotencyKey: op.ExternalIdempotencyKey, Payload: append([]byte(nil), op.MappedPayload...)})
	received := j.now().UTC()
	j.mu.Lock()
	defer j.mu.Unlock()
	current := j.operations[op.OperationID]
	if current.State != StateSending || current.FenceToken != lease.FenceToken {
		return DispatchResult{}, ErrLeaseFenced
	}
	delete(j.activeLeases, lease.OperationID)
	attempt := Attempt{AttemptID: uuid.New(), OperationID: op.OperationID, AttemptNumber: len(current.Attempts) + 1, RequestDigest: current.MappedPayloadDigest, FenceToken: lease.FenceToken, AttemptedAt: now, ReceivedAt: received}
	if callErr != nil {
		var pe *ProviderError
		_ = errors.As(callErr, &pe)
		ambiguous := errors.Is(callErr, context.DeadlineExceeded) || (pe != nil && pe.MayHaveSent) || (pe != nil && pe.Result == ResponseAmbiguous)
		if ambiguous {
			attempt.ProviderResult, attempt.RetryDisposition, current.State, current.ResponseClass = ResponseAmbiguous, RetryObservationNeeded, StateAmbiguous, ResponseAmbiguous
		} else {
			attempt.ProviderResult, attempt.RetryDisposition, current.State, current.ResponseClass = ResponseFailure, Retry, StateFailed, ResponseFailure
		}
		current.Attempts = append(current.Attempts, attempt)
		current.UpdatedAt = received
		j.operations[current.OperationID] = cloneOperation(current)
		j.appendJournalLocked(current, StateSending, current.State, "ATTEMPT_RECORDED", attempt.AttemptID, lease.FenceToken, "", attempt.RequestDigest, received)
		return DispatchResult{Operation: cloneOperation(current), Attempt: attempt, ProviderCall: true, ObservationRequired: ambiguous}, callErr
	}
	if response.Result == "" {
		response.Result = ResponseSuccess
	}
	attempt.ProviderResult, attempt.ProviderRequestID, attempt.ResponseDigest = response.Result, response.ProviderRequestID, response.ResponseDigest
	attempt.RetryDisposition = RetryObservationNeeded
	current.Attempts = append(current.Attempts, attempt)
	current.State = StateProviderAccepted
	current.ResponseClass = response.Result
	current.UpdatedAt = received
	j.operations[current.OperationID] = cloneOperation(current)
	j.appendJournalLocked(current, StateSending, current.State, "ATTEMPT_RECORDED", attempt.AttemptID, lease.FenceToken, "", attempt.RequestDigest, received)
	return DispatchResult{Operation: cloneOperation(current), Attempt: attempt, ProviderCall: true, ObservationRequired: true}, nil
}

// RecordObservation resolves an accepted or ambiguous operation using a
// typed provider observation. UNKNOWN escalates to repair; it never succeeds.
func (j *MemoryJournal) RecordObservation(ctx context.Context, obs Observation) (Operation, error) {
	if err := contextError(ctx); err != nil {
		return Operation{}, err
	}
	if err := obs.validate(); err != nil {
		return Operation{}, err
	}
	if strings.TrimSpace(obs.TenantID) == "" {
		return Operation{}, fmt.Errorf("%w: tenant is required", ErrObservationRequired)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	op, ok := j.operations[obs.OperationID]
	if !ok || obs.TenantID != op.TenantID {
		return Operation{}, fmt.Errorf("%w: %s", ErrNotFound, obs.OperationID)
	}
	if op.ObservationRequirement != ObservationBySemanticIdentity {
		return Operation{}, fmt.Errorf("%w: unsupported requirement %q", ErrObservationRequired, op.ObservationRequirement)
	}
	if op.ExternalResourceKey != obs.ExternalResourceKey {
		return Operation{}, fmt.Errorf("%w: observation resource does not match operation", ErrObservationRequired)
	}
	if op.State != StateAmbiguous && op.State != StateProviderAccepted && op.State != StateObserving {
		return Operation{}, fmt.Errorf("%w: state is %s", ErrObservationRequired, op.State)
	}
	from := op.State
	op.State = StateObserving
	op.ObservationID = obs.ObservationID
	op.UpdatedAt = obs.ObservedAt.UTC()
	op.ReconciliationStatus = string(obs.Verdict)
	switch obs.Verdict {
	case ObservationApplied:
		op.State, op.CompletionState, op.ResponseClass = StateReconciled, "COMPLETE", ResponseSuccess
	case ObservationNotApplied:
		op.State, op.CompletionState, op.ResponseClass = StateRetryable, "PENDING", ResponseFailure
	case ObservationConflict:
		op.State, op.CompletionState, op.ResponseClass = StateRepairRequired, "PENDING", ResponseFailure
	case ObservationUnknown:
		op.State, op.CompletionState, op.ResponseClass = StateRepairRequired, "PENDING", ResponseUnknown
	}
	op.Attempts = append([]Attempt(nil), op.Attempts...)
	j.operations[op.OperationID] = cloneOperation(op)
	j.appendJournalLocked(op, from, op.State, "OBSERVATION_RECORDED", uuid.Nil, op.FenceToken, "", obs.ObservedDigest, op.UpdatedAt)
	return cloneOperation(op), nil
}

// PreviewRedrive compares the original semantic bindings to current state.
func (j *MemoryJournal) PreviewRedrive(ctx context.Context, req RedriveRequest) (RedrivePreview, error) {
	if err := contextError(ctx); err != nil {
		return RedrivePreview{}, err
	}
	if strings.TrimSpace(req.TenantID) == "" {
		return RedrivePreview{}, fmt.Errorf("%w: tenant is required", ErrInvalid)
	}
	j.mu.RLock()
	op, ok := j.operations[req.OperationID]
	j.mu.RUnlock()
	if !ok || req.TenantID != op.TenantID {
		return RedrivePreview{}, fmt.Errorf("%w: %s", ErrNotFound, req.OperationID)
	}
	if op.State != StateFailed && op.State != StateRetryable && op.State != StateRepairRequired {
		return RedrivePreview{}, fmt.Errorf("%w: state is %s", ErrRedriveNotAllowed, op.State)
	}
	var changes []string
	if req.CurrentMappingProfileVersion != "" && req.CurrentMappingProfileVersion != op.MappingProfileVersion {
		changes = append(changes, "MAPPING")
	}
	if req.CurrentMappedPayloadDigest != "" && req.CurrentMappedPayloadDigest != op.MappedPayloadDigest {
		changes = append(changes, "PAYLOAD")
	}
	if req.CurrentAuthorityPolicyFingerprint != "" && req.CurrentAuthorityPolicyFingerprint != op.AuthorityPolicyFingerprint {
		changes = append(changes, "AUTHORITY")
	}
	if req.CurrentWriterFenceEpoch != 0 && req.CurrentWriterFenceEpoch != op.WriterFenceEpoch {
		changes = append(changes, "WRITER_FENCE")
	}
	if req.CurrentCredentialRef != "" && req.CurrentCredentialRef != op.CredentialRef {
		changes = append(changes, "CREDENTIAL")
	}
	return RedrivePreview{OperationID: op.OperationID, Compatible: len(changes) == 0, RequiresApproval: len(changes) > 0, MaterialChanges: append([]string(nil), changes...), Explanation: fmt.Sprintf("redrive operation=%s compatible=%t changes=%v", op.OperationID, len(changes) == 0, changes)}, nil
}

// Redrive queues only the selected failed operation. It preserves operation
// identity, semantic input, idempotency key and all siblings.
func (j *MemoryJournal) Redrive(ctx context.Context, req RedriveRequest) (Operation, error) {
	preview, err := j.PreviewRedrive(ctx, req)
	if err != nil {
		return Operation{}, err
	}
	if req.ActorRef == "" {
		return Operation{}, fmt.Errorf("%w: actor is required", ErrInvalid)
	}
	if preview.RequiresApproval && !req.Approved {
		return Operation{}, fmt.Errorf("%w: changes=%v", ErrRedriveApprovalRequired, preview.MaterialChanges)
	}
	if preview.RequiresApproval && req.RepairPlanID == "" {
		return Operation{}, fmt.Errorf("%w: changes=%v", ErrRepairPlanRequired, preview.MaterialChanges)
	}
	at := req.At.UTC()
	if at.IsZero() {
		at = j.now().UTC()
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	op := j.operations[req.OperationID]
	if op.State != StateFailed && op.State != StateRetryable && op.State != StateRepairRequired {
		return Operation{}, fmt.Errorf("%w: state changed to %s", ErrRedriveNotAllowed, op.State)
	}
	if op.State == StateRepairRequired && req.RepairPlanID == "" {
		return Operation{}, fmt.Errorf("%w: repair-required operation", ErrRepairPlanRequired)
	}
	op.State, op.ResponseClass, op.UpdatedAt = StateQueued, ResponsePending, at
	op.RedriveCount++
	record := RedriveRecord{RedriveID: uuid.New(), OperationID: op.OperationID, ActorRef: req.ActorRef, RepairPlanID: req.RepairPlanID, At: at, MaterialChanges: append([]string(nil), preview.MaterialChanges...)}
	j.redrives[op.OperationID] = append(j.redrives[op.OperationID], record)
	j.operations[op.OperationID] = cloneOperation(op)
	j.appendJournalLocked(op, StateFailed, StateQueued, "REDRIVE_QUEUED", uuid.Nil, op.FenceToken, "", "", at)
	return cloneOperation(op), nil
}

// Redrives returns immutable redrive lineage for one operation.
func (j *MemoryJournal) Redrives(ctx context.Context, tenant string, id uuid.UUID) ([]RedriveRecord, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(tenant) == "" {
		return nil, fmt.Errorf("%w: tenant is required", ErrInvalid)
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	if op, ok := j.operations[id]; !ok || op.TenantID != tenant {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	out := append([]RedriveRecord(nil), j.redrives[id]...)
	out = append([]RedriveRecord(nil), out...)
	return out, nil
}

func (o Operation) validate() error {
	required := map[string]string{"tenant": o.TenantID, "connection": o.ConnectionID, "connector_version": o.ConnectorVersion, "semantic_operation": o.SemanticOperation, "direction": o.Direction, "criticality": o.Criticality, "resource_key": o.ExternalResourceKey, "authority_ref": o.SourceAuthorityDecisionRef, "authority_fingerprint": o.AuthorityPolicyFingerprint, "canonical_input_digest": o.CanonicalInputDigest, "mapping_version": o.MappingProfileVersion, "mapped_payload_digest": o.MappedPayloadDigest, "classification": o.Classification, "purpose": o.Purpose, "destination": o.DestinationRef, "credential_ref": o.CredentialRef, "idempotency_key": o.IdempotencyKey}
	for name, value := range required {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s is required", ErrInvalid, name)
		}
	}
	if o.OperationID == uuid.Nil || o.WriterFenceEpoch == 0 || o.DeadlineAt.IsZero() {
		return fmt.Errorf("%w: id, writer fence and deadline are required", ErrInvalid)
	}
	if o.OrderingClass != OrderingStrict && o.OrderingClass != OrderingCommutative && o.OrderingClass != OrderingIndependent && o.OrderingClass != OrderingProviderConditional {
		return fmt.Errorf("%w: invalid ordering class %q", ErrInvalid, o.OrderingClass)
	}
	if o.OrderingClass == OrderingStrict || o.OrderingClass == OrderingProviderConditional {
		if o.ResourceSequence == 0 {
			return fmt.Errorf("%w: ordered operation requires resource sequence", ErrInvalid)
		}
	}
	if o.ObservationRequirement != ObservationBySemanticIdentity {
		return fmt.Errorf("%w: unsupported observation requirement", ErrInvalid)
	}
	return nil
}

func (o Observation) validate() error {
	if o.ObservationID == uuid.Nil || o.OperationID == uuid.Nil || strings.TrimSpace(o.ExternalResourceKey) == "" || o.ObservedAt.IsZero() {
		return fmt.Errorf("%w: observation id, operation, resource and time are required", ErrObservationRequired)
	}
	switch o.Verdict {
	case ObservationApplied, ObservationNotApplied, ObservationConflict, ObservationUnknown:
	default:
		return fmt.Errorf("%w: invalid verdict %q", ErrObservationRequired, o.Verdict)
	}
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("%w: context is required", ErrInvalid)
	}
	return ctx.Err()
}

func cloneOperation(o Operation) Operation {
	o.MappedPayload = append([]byte(nil), o.MappedPayload...)
	o.Attempts = append([]Attempt(nil), o.Attempts...)
	return o
}

// Explain returns a bounded, payload-free account of an operation.
func Explain(o Operation) string {
	return fmt.Sprintf("connector operation v%d id=%s state=%s resource=%s sequence=%d response=%s attempts=%d redrives=%d completion=%s", Version(), o.OperationID, o.State, o.ExternalResourceKey, o.ResourceSequence, o.ResponseClass, len(o.Attempts), o.RedriveCount, o.CompletionState)
}

// PayrollSync is a deterministic external-write fixture for the Promotion
// pilot. It honors the semantic idempotency key and records no duplicate write.
type PayrollSync struct {
	mu               sync.Mutex
	writes           map[string]WriteResponse
	calls            []WriteRequest
	TimeoutAfterSend bool
}

// NewPayrollSync returns an empty payroll-sync fixture.
func NewPayrollSync() *PayrollSync { return &PayrollSync{writes: make(map[string]WriteResponse)} }

// Write implements Writer.
func (p *PayrollSync) Write(ctx context.Context, req WriteRequest) (WriteResponse, error) {
	if err := contextError(ctx); err != nil {
		return WriteResponse{}, err
	}
	if req.IdempotencyKey == "" || req.ExternalResourceKey == "" {
		return WriteResponse{}, fmt.Errorf("%w: payroll request identity is incomplete", ErrInvalid)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, WriteRequest{OperationID: req.OperationID, SemanticOperation: req.SemanticOperation, ExternalResourceKey: req.ExternalResourceKey, ExpectedExternalVersion: req.ExpectedExternalVersion, IdempotencyKey: req.IdempotencyKey, Payload: append([]byte(nil), req.Payload...)})
	if existing, ok := p.writes[req.IdempotencyKey]; ok {
		return existing, nil
	}
	if p.TimeoutAfterSend {
		return WriteResponse{}, &ProviderError{Result: ResponseAmbiguous, MayHaveSent: true, Cause: context.DeadlineExceeded}
	}
	response := WriteResponse{Result: ResponseSuccess, ProviderRequestID: "payroll-" + req.IdempotencyKey, ExternalObjectRef: req.ExternalResourceKey, ExternalVersion: "v1"}
	p.writes[req.IdempotencyKey] = response
	return response, nil
}

// Calls returns provider calls, including attempts that ended ambiguously.
func (p *PayrollSync) Calls() []WriteRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := append([]WriteRequest(nil), p.calls...)
	for i := range out {
		out[i].Payload = append([]byte(nil), out[i].Payload...)
	}
	return out
}
