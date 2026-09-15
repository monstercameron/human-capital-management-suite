package execute

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// Beginner opens the transactions that fence Start and each Advance. A pool
// or a dedicated connection can satisfy it.
type Beginner interface {
	Begin(ctx context.Context) (dbport.Tx, error)
}

// SerializableBeginner is implemented by production PostgreSQL pools. It is
// optional so legacy callers retain their existing transaction behavior.
type SerializableBeginner interface {
	BeginSerializable(ctx context.Context) (dbport.Tx, error)
}

// ReadOnlyBeginner opens an explicit read-only transaction for resolving an
// ambiguous START outcome. It is deliberately separate from Beginner so a
// normal transaction can never be mistaken for an ambiguity resolver.
type ReadOnlyBeginner interface {
	BeginReadOnly(ctx context.Context) (dbport.Tx, error)
}

// TransactionalWorkflowResolver performs authoritative workflow selection
// through the same serializable snapshot that Start will commit. Retry mode
// requires this interface; lexical execution inside a closure is not enough
// when the resolver reads through another connection.
type TransactionalWorkflowResolver interface {
	ResolveWorkflowInTx(context.Context, dbport.Tx, runtime.StartRequest) (runtime.WorkflowSelection, error)
}

// AdvanceFunc is the runtime advancement boundary. Production wiring uses
// runtime.Advance; naming the function lets the driver be tested without
// pretending an in-memory transaction is PostgreSQL.
type AdvanceFunc func(context.Context, runtime.Executor, runtime.AdvanceRequest) (runtime.AdvanceReceipt, error)

// StepRequest is the complete runtime context handed to one synchronous READY
// step. Proposal content is immutable and the compiled node is from the exact
// plan Start pinned.
type StepRequest struct {
	TenantID        uuid.UUID
	InstanceID      uuid.UUID
	InstanceVersion int64
	Attempt         int
	Node            workflow.CompiledNode
	Plan            *workflow.CompiledWorkflow
	Proposal        runtime.ProposalBinding
	CorrelationID   string
	RecordedAt      time.Time
	// TraceID is the ambient trace id the driver read off the incoming span
	// context (OBS-023). Empty when the caller carried no trace context.
	TraceID string
}

// StepRunner executes one READY node and returns only its typed outcome and
// governance references. It must not schedule a successor or create human
// work; runtime.Advance derives those continuations from the pinned plan.
type StepRunner interface {
	Run(ctx context.Context, req StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error)
}

// TransactionalStepRunner is an optional [StepRunner] extension for nodes
// whose effect must commit atomically with the node's advancement. For a node
// RunsInTransaction claims, the driver calls RunInTx inside the same tenant
// transaction that records the outcome and derives continuations, instead of
// calling Run before that transaction opens. An error or a failed commit
// rolls back the step's writes together with the advancement.
type TransactionalStepRunner interface {
	StepRunner
	RunsInTransaction(node workflow.CompiledNode) bool
	RunInTx(ctx context.Context, ex runtime.Executor, req StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error)
}

// WorkItemRequest asks the human-work adapter to create and route the item a
// WORK_ITEM_REQUIRED continuation describes. WorkItemID is deterministic and
// must be used as supplied.
type WorkItemRequest struct {
	WorkItemID    uuid.UUID
	Continuation  runtime.ContinuationRecord
	Proposal      runtime.ProposalBinding
	CellID        string
	CorrelationID string
	SubjectRefs   []string
	CreatedAt     time.Time
}

// WorkItemFactory owns the policy-specific fields and assignment resolution a
// real WorkItem requires. Its writes run through ex, the same transaction as
// the runtime advancement that raised the continuation.
type WorkItemFactory interface {
	CreateAndRoute(ctx context.Context, ex workitem.Executor, req WorkItemRequest) (workitem.WorkItem, error)
}

// TerminalWriteRequest is the governed business-terminal write. The writer
// must append its ledger event, apply its projection and enqueue its outbox
// message through tx, and return the identities TX-006 stores for replay.
type TerminalWriteRequest struct {
	TenantID       uuid.UUID
	InstanceID     uuid.UUID
	WorkflowID     string
	PlanDigest     string
	Proposal       runtime.ProposalBinding
	TerminalCode   string
	CorrelationID  string
	IdempotencyKey string
	RecordedAt     time.Time
	// EndNodeID and EndOutputDigest are the completed END node and its typed
	// output digest (WF-RUN-030), recorded verbatim from the outcome that
	// reached this terminal rather than discarded before the write.
	EndNodeID       string
	EndOutputDigest string
}

// TerminalWriter performs the single governed terminal write inside the
// transaction supplied by the driver. It must have no out-of-transaction side
// channel.
type TerminalWriter interface {
	Write(ctx context.Context, tx dbport.Tx, req TerminalWriteRequest) (idempotency.ResultIdentity, error)
}

// RepairRequest is the durable follow-up requested when a terminal reports a
// committed business fact whose downstream consistency is degraded.
type RepairRequest struct {
	TenantID     uuid.UUID
	InstanceID   uuid.UUID
	WorkflowID   string
	PlanDigest   string
	Proposal     runtime.ProposalBinding
	EffectRef    string
	PolicyRef    string
	IntendedRef  string
	RepairPolicy string
	RequestedAt  time.Time
}

// RepairRequester records the reconciliation promise in the same transaction
// as the terminal write. A nil requester preserves the historical behavior for
// terminals that do not require repair.
type RepairRequester interface {
	Request(ctx context.Context, tx dbport.Tx, req RepairRequest) error
}

// WorkItemReader loads the durable WorkItem a [Driver.Resume] advances from,
// inside the same transaction as the advancement it feeds -- WF-RUN-028's
// replacement for a [ResumeRequest] that carried a caller-assembled
// [workitem.WorkItem] struct. internal/platform/execution's thin adapter over
// internal/humanwork/workitem.Store is the production implementation; a test
// composes its own double.
type WorkItemReader interface {
	Load(ctx context.Context, ex workitem.Executor, tenantID, workItemID uuid.UUID) (workitem.WorkItem, error)
}
