package inspect

import (
	"encoding/json"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/cancellation"
)

// RecordState says how one durable record family behind a [DurableView] was
// read. It exists so a family the inspector could not read is never rendered
// as an empty list that reads as "nothing happened".
type RecordState string

// The record states.
const (
	// RecordLoaded reports a family read from its durable store with at
	// least one row for this instance.
	RecordLoaded RecordState = "LOADED"
	// RecordNotRecorded reports a family read from its durable store that
	// holds nothing for this instance. That is a fact, not a failure.
	RecordNotRecorded RecordState = "NOT_RECORDED"
	// RecordRedacted reports a family withheld from this caller.
	RecordRedacted RecordState = "REDACTED"
	// RecordIntegrityFailed reports a family whose stored rows no longer
	// verify against the digest they were sealed with. The rows are still
	// rendered, marked unverified, and the failure is a named gap.
	RecordIntegrityFailed RecordState = "INTEGRITY_FAILED"
	// RecordUnavailable reports a family the inspector cannot read at all:
	// no durable store or no durable link to a workflow instance exists in
	// this repository, or the store was not configured for this read.
	RecordUnavailable RecordState = "UNAVAILABLE"
)

// Durable record families, in the order a [DurableView] lists them. The name
// is the table (or, for trace, the column set) the family is read from.
const (
	FamilyInstance             = "workflow_instance"
	FamilyCompiledVersion      = "workflow_compiled_version"
	FamilyExecutionContext     = "workflow_execution_context"
	FamilyNodeExecution        = "workflow_node_execution"
	FamilyTimer                = "workflow_timer"
	FamilyAdvancementReceipt   = "workflow_advancement_receipt"
	FamilyWorkItem             = "work_item"
	FamilyLease                = "workflow_lease"
	FamilyCheckpoint           = "workflow_checkpoint"
	FamilyBusinessTransaction  = "business_transaction"
	FamilyOutbox               = "outbox"
	FamilyConnectorOperation   = "connector_operation"
	FamilyReconciliation       = "effect_reconciliation_job"
	FamilyCancellationDecision = "workflow_cancellation_decision"
	FamilyTrace                = "trace_links"
)

// Unavailability reasons for the families no durable store in this
// repository can answer for a workflow instance. They are stated, not
// guessed around: rendering them as empty would claim the inspector looked.
const (
	// ReasonNoBusinessTransactionStore: workflow_instance.business_transaction_id
	// is a bare identifier; no table in this repository is keyed by it.
	ReasonNoBusinessTransactionStore = "NO_DURABLE_STORE_KEYED_BY_BUSINESS_TRANSACTION_ID"
	// ReasonNoConnectorOperationLink: connector_operation carries a
	// workflow_ref and an effect_ref, but no workflow writer populates either
	// for an instance, so there is nothing to traverse to.
	ReasonNoConnectorOperationLink = "NO_WORKFLOW_WRITER_LINKS_CONNECTOR_OPERATION"
	// ReasonVersionRegistryNotConfigured: the caller supplied no durable
	// version registry to resolve the instance's pin against.
	ReasonVersionRegistryNotConfigured = "VERSION_REGISTRY_NOT_CONFIGURED"
)

// RecordFamily is one line of the durable read's manifest.
type RecordFamily struct {
	Family  string      `json:"family"`
	Section Section     `json:"section"`
	State   RecordState `json:"state"`
	Count   int         `json:"count"`
	Reason  string      `json:"reason,omitempty"`
}

// VersionApprovalView is one lifecycle transition of the pinned version.
type VersionApprovalView struct {
	Result     string    `json:"result"`
	ApprovedBy string    `json:"approved_by"`
	Authority  string    `json:"authority"`
	ApprovedAt time.Time `json:"approved_at"`
}

// VersionRecordView is the durable registry record of the compiled version
// the instance pinned: its record digest (re-verified on read), current
// lifecycle status and approval history.
type VersionRecordView struct {
	State              RecordState           `json:"state"`
	Reason             string                `json:"reason,omitempty"`
	CompiledPlanDigest string                `json:"compiled_plan_digest,omitempty"`
	WorkflowID         string                `json:"workflow_id,omitempty"`
	DefinitionVersion  uint32                `json:"definition_version,omitempty"`
	SemanticVersion    string                `json:"semantic_version,omitempty"`
	DefinitionDigest   string                `json:"definition_digest,omitempty"`
	RecordDigest       string                `json:"record_digest,omitempty"`
	Status             string                `json:"status,omitempty"`
	PublishedBy        string                `json:"published_by,omitempty"`
	PublishedAt        *time.Time            `json:"published_at,omitempty"`
	Approvals          []VersionApprovalView `json:"approvals"`
	// PinMatches reports that the registry record names the same workflow
	// and definition version the instance row pinned.
	PinMatches bool `json:"pin_matches"`
}

// ExecutionContextView is the pinned WorkflowExecutionContext. Its content --
// principal, organization, legal and entitlement context -- is protected and
// never rendered; the digest is rendered under
// [FieldInstanceContext], and the runtime and mode facts are operational.
type ExecutionContextView struct {
	State             RecordState `json:"state"`
	Reason            string      `json:"reason,omitempty"`
	ContextDigest     Ref         `json:"context_digest"`
	Verified          bool        `json:"verified"`
	RuntimeVersion    string      `json:"runtime_version,omitempty"`
	ExecutionMode     string      `json:"execution_mode,omitempty"`
	PlanDigestMatches bool        `json:"plan_digest_matches"`
}

// AttemptHistory is one node's attempts and retry state, read from its node
// execution rows and its RETRY_BACKOFF timers.
type AttemptHistory struct {
	NodeID         string `json:"node_id"`
	Current        bool   `json:"current"`
	Attempts       int    `json:"attempts"`
	LatestAttempt  int    `json:"latest_attempt"`
	LatestStatus   string `json:"latest_status"`
	RetryPolicyRef Ref    `json:"retry_policy_ref"`
	// RetryTimers counts the node's durable RETRY_BACKOFF timers in any
	// state; NextRetryAt is the earliest one still PENDING, which is a
	// durable promise the timer scheduler holds, not an estimate.
	RetryTimers int        `json:"retry_timers"`
	NextRetryAt *time.Time `json:"next_retry_at,omitempty"`
}

// TimerView is one durable workflow_timer row.
type TimerView struct {
	TimerID   string    `json:"timer_id"`
	NodeID    string    `json:"node_id"`
	Kind      string    `json:"kind"`
	State     string    `json:"state"`
	Key       string    `json:"key"`
	FiresAt   time.Time `json:"fires_at"`
	Version   uint64    `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

// ReceiptView is one durable advancement receipt.
type ReceiptView struct {
	NodeID                   string    `json:"node_id"`
	Attempt                  int       `json:"attempt"`
	ExpectedInstanceVersion  int64     `json:"expected_instance_version"`
	ResultingInstanceVersion int64     `json:"resulting_instance_version"`
	RequestDigest            string    `json:"request_digest"`
	ReceiptDigest            string    `json:"receipt_digest"`
	DigestVerified           bool      `json:"digest_verified"`
	CompletedState           string    `json:"completed_state"`
	RouteKey                 string    `json:"route_key,omitempty"`
	OutputDigest             Ref       `json:"output_digest"`
	Frontier                 []string  `json:"frontier"`
	Complete                 bool      `json:"complete"`
	TerminalCode             string    `json:"terminal_code,omitempty"`
	RecordedAt               time.Time `json:"recorded_at"`
}

// LeaseTransitionView is one reconstructed lease transition of the instance.
type LeaseTransitionView struct {
	Kind     string    `json:"kind"`
	LeaseID  string    `json:"lease_id"`
	HolderID string    `json:"holder_id"`
	Token    uint64    `json:"token"`
	At       time.Time `json:"at"`
}

// LeaseView is the instance's WORKFLOW_INSTANCE lease history.
type LeaseView struct {
	State       RecordState           `json:"state"`
	Transitions []LeaseTransitionView `json:"transitions"`
	// Held reports whether the latest lease is still open; HolderID and
	// Token then name who holds it under which fence.
	Held     bool   `json:"held"`
	HolderID string `json:"holder_id,omitempty"`
	Token    uint64 `json:"token,omitempty"`
}

// CheckpointView is the instance's latest durable safe point.
type CheckpointView struct {
	State           RecordState `json:"state"`
	Sequence        uint64      `json:"sequence,omitempty"`
	Kind            string      `json:"kind,omitempty"`
	StateDigest     string      `json:"state_digest,omitempty"`
	FrontierDigest  string      `json:"frontier_digest,omitempty"`
	VariableDigest  string      `json:"variable_digest,omitempty"`
	InstanceVersion uint64      `json:"instance_version,omitempty"`
	TakenAt         *time.Time  `json:"taken_at,omitempty"`
}

// OutboxView is the outbox row an effect enqueued. The payload is never
// loaded into the view and the last error is reported only as present or
// not: both can carry protected content.
type OutboxView struct {
	OutboxID          string    `json:"outbox_id"`
	Status            string    `json:"status"`
	Attempts          int       `json:"attempts"`
	OrderingKey       string    `json:"ordering_key"`
	SchemaRef         string    `json:"schema_ref"`
	Criticality       string    `json:"criticality"`
	AvailableAt       time.Time `json:"available_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	LastErrorRecorded bool      `json:"last_error_recorded"`
}

// ReconciliationView is one effect_reconciliation_job watching an effect.
type ReconciliationView struct {
	JobID               string    `json:"job_id"`
	PolicyRef           string    `json:"policy_ref"`
	Status              string    `json:"status"`
	IntendedRef         string    `json:"intended_ref"`
	CanonicalRef        string    `json:"canonical_ref,omitempty"`
	RequiredFreshness   string    `json:"required_freshness"`
	ObservationAttempts int       `json:"observation_attempts"`
	NextCheckAt         time.Time `json:"next_check_at"`
	Deadline            time.Time `json:"deadline"`
	RepairPolicy        string    `json:"repair_policy"`
	Owner               string    `json:"owner"`
}

// EffectView is one effect reference the instance's nodes recorded, followed
// to the outbox row and the reconciliation jobs keyed by it.
type EffectView struct {
	EffectRef           string               `json:"effect_ref"`
	NodeIDs             []string             `json:"node_ids"`
	OutboxState         RecordState          `json:"outbox_state"`
	Outbox              *OutboxView          `json:"outbox,omitempty"`
	ReconciliationState RecordState          `json:"reconciliation_state"`
	Reconciliation      []ReconciliationView `json:"reconciliation"`
}

// DurableView is the inspection [Load] reads from PostgreSQL: the governed
// traversal [Build] renders, plus every durable record family behind it.
//
// Completeness merges the traversal's, the work items' and the durable
// read's own redactions and gaps. Unavailable names the families no durable
// store in this repository can answer; they are listed in Records too, and
// they do not make a view incomplete, because nothing the instance recorded
// was missed.
type DurableView struct {
	View             View                    `json:"view"`
	Records          []RecordFamily          `json:"records"`
	Version          VersionRecordView       `json:"version"`
	ExecutionContext ExecutionContextView    `json:"execution_context"`
	Attempts         []AttemptHistory        `json:"attempts"`
	Timers           []TimerView             `json:"timers"`
	Receipts         []ReceiptView           `json:"receipts"`
	WorkItems        WorkItemsResult         `json:"work_items"`
	Lease            LeaseView               `json:"lease"`
	Checkpoint       CheckpointView          `json:"checkpoint"`
	Effects          []EffectView            `json:"effects"`
	Cancellation     cancellation.Settlement `json:"cancellation_settlement"`
	TraceIDs         RefList                 `json:"trace_ids"`
	Completeness     Completeness            `json:"completeness"`
	Unavailable      []string                `json:"unavailable"`
}

// JSON renders the durable view as indented JSON in declaration order, the
// same way [View.JSON] does.
func (v DurableView) JSON() ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Record returns the manifest line for one family and whether it is listed.
func (v DurableView) Record(family string) (RecordFamily, bool) {
	for _, r := range v.Records {
		if r.Family == family {
			return r, true
		}
	}
	return RecordFamily{}, false
}
