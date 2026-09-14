package app

import (
	"context"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	ledgerport "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

// ErrIntentNotFound is what a [Store] returns when no intent is visible at the
// requested identity. The service projects it as a typed NOT_FOUND; a store
// must never invent an empty record instead.
var ErrIntentNotFound = errors.New("app: intent not found")

// IntentRecord is one persisted intent as this package reads and writes it.
//
// Envelope carries the authoritative bytes: the marshalled
// hcmnext.intents.v1.IntentInstance the ledger event holds. Every other field
// is a projection of those bytes, present so that a list or a lookup does not
// have to decode the envelope to answer.
type IntentRecord struct {
	Tenant     string
	IntentID   string
	Definition intent.Ref

	IdempotencyKey string
	CorrelationID  string

	Lifecycle       lifecycle.Dimensions
	InstanceVersion uint64
	RequestDigest   digest.Reference

	CreatedAt        time.Time
	RecordedAt       time.Time
	LastTransitionAt time.Time

	// CommitReceiptRef and RepairRef are the terminal references BindOutcome
	// projected beside the lifecycle tuple; empty until a terminal is bound.
	CommitReceiptRef string
	RepairRef        string
	LegalEvidence    *intent.LegalObligationEvidence

	// Envelope is the marshalled hcmnext.intents.v1.IntentInstance.
	Envelope []byte
	// EnvelopeSchemaRef is the registered payload-schema reference the ledger
	// event and the outbox message declare for Envelope.
	EnvelopeSchemaRef string
}

// AppendResult is what one [Store.AppendIntent] call produced.
//
// Replayed reports that the idempotency key was already recorded with the same
// bytes: Record is then the originally stored record, no new ledger event was
// appended, the projection did not advance again and no second outbox message
// was enqueued.
type AppendResult struct {
	Record            IntentRecord
	Ledger            ledgerport.AppendReceipt
	Replayed          bool
	ProjectionApplied bool
	OutboxID          string
	StreamKey         string
	ProjectionName    string
}

// TimelineEntry is one entry of an intent's chronology, read back from the
// authoritative ledger rather than from a status column.
type TimelineEntry struct {
	EventID         string
	Kind            string
	Sequence        int64
	SchemaRef       string
	Digest          string
	DigestAlgorithm string
	OccurredAt      time.Time
	RecordedAt      time.Time
}

// IntentPage is one page of a list answer plus the cursor that continues it.
type IntentPage struct {
	Records    []IntentRecord
	NextCursor string
}

// Store is the persistence port this package depends on. The PostgreSQL
// implementation is internal/intent/app/pgstore; this package never imports
// it, so the application layer stays free of SQL, pgx and migration knowledge.
//
// The contract an implementation owes:
//
//   - Bootstrap is idempotent and safe to call on every start.
//   - AppendIntent commits the ledger event, the projection advance and the
//     outbox message in ONE transaction, or none of them. A replay of the same
//     idempotency key returns the original record with Replayed true, and
//     appends, advances and enqueues nothing.
//   - The read methods perform no write of any kind.
type Store interface {
	// Bootstrap registers whatever a tenant needs before its first append:
	// the tenant row, the payload schema the envelope declares, and any
	// other registration the adapter's own append contract requires.
	Bootstrap(ctx context.Context, tenant string) error

	// AppendIntent records one drafted instance as chronology.
	AppendIntent(ctx context.Context, rec IntentRecord) (AppendResult, error)

	// LoadIntent returns one record, or ErrIntentNotFound.
	LoadIntent(ctx context.Context, tenant, intentID string) (IntentRecord, error)

	// ListIntents returns one bounded page of a tenant's intents, ordered by
	// creation, continuing from cursor when it is non-empty.
	ListIntents(ctx context.Context, tenant string, pageSize int32, cursor string) (IntentPage, error)

	// Timeline returns the intent's ledger chronology in sequence order.
	Timeline(ctx context.Context, tenant, intentID string) ([]TimelineEntry, error)
}

// ErrLifecycleWritesUnavailable is returned by [IntentService.SubmitIntent],
// [IntentService.CancelIntent] and [IntentService.SupersedeIntent] when this
// cell's composed [Store] does not implement [LifecycleMutator]. It leaves
// these RPCs answering UNAVAILABLE rather than reporting a false success:
// exactly the treatment a nil [OutcomeBinder] already gives ExecuteIntent's
// terminal write.
var ErrLifecycleWritesUnavailable = errors.New("app: this cell's store does not support lifecycle-mutating writes")

// LifecycleMutation is one governed lifecycle transition beyond creation:
// Submit, Cancel and Supersede all advance the five projected lifecycle
// columns (and, when applicable, the commit/repair references) under a
// compare-and-swap against the caller's own last-read instance version,
// exactly like [OutcomeBinder]'s terminal write. The intent's own envelope —
// the immutable creation fact a ledger event carries — is never rewritten by
// a lifecycle mutation; only the mutable projection moves.
type LifecycleMutation struct {
	Tenant                  string
	IntentID                string
	ExpectedInstanceVersion uint64
	Lifecycle               lifecycle.Dimensions
	// CommitReceiptRef and RepairRef mirror the fields [OutcomeBinder] already
	// projects; a mutation that does not touch one leaves it as the store's
	// current value (an empty string here means "no change", not "clear it").
	CommitReceiptRef string
	RepairRef        string
	RecordedAt       time.Time
}

// LifecycleMutator is implemented by a [Store] that supports the durable
// governed writes SubmitIntent, CancelIntent and SupersedeIntent perform
// beyond creation. It is optional exactly like [OutcomeBinder]: an adapter
// that does not implement it leaves these three RPCs answering UNAVAILABLE
// once past authorization and idempotency, rather than silently no-op
// persisting a transition the caller was told succeeded.
type LifecycleMutator interface {
	MutateLifecycle(ctx context.Context, m LifecycleMutation) (IntentRecord, error)
}

// SafePoints resolves whether the workflow execution currently bound to an
// intent is stopped at a declared safe point (internal/workflow/compile.go's
// placeSafePoints/Node.SafePoint, WF-RUN-010) right now, and whether a
// partial effect landed there that cancellation cannot cleanly reverse.
//
// It is a port, not a direct workflow-runtime read, because
// internal/intent/app must not import the workflow runtime's mutable
// execution tables — exactly the same layering reason [ProposalExecutor] is
// a narrow port rather than a concrete driver. A cell composed with a nil
// SafePoints leaves [IntentService.CancelIntent] treating every EXECUTING
// intent as [intent.CancellationPointUnknown]: it never claims a clean
// cancel it cannot prove.
type SafePoints interface {
	// At answers the safe-point question for the workflow execution bound to
	// intentID. repairRef is populated only when point is
	// [intent.CancellationPointPartialEffect]; it names the RepairPlan or
	// incident the resulting REPAIR_REQUIRED tuple binds.
	At(ctx context.Context, tenant, intentID string) (point intent.CancellationPoint, repairRef string, err error)
}

// DomainCall is everything the service needs to answer one P1A intent: the
// kernel's baseline snapshot, the governed read that must run first, and the
// one typed domain request the owning domain package consumes.
//
// Exactly one of Promotion, Compensation, PayBand, Drift, Repair and
// Transaction is set, or none of them when Explain is itself the whole answer
// (explain_worker_state). A union struct rather than an `any` keeps the set of
// P1A domain calls enumerable: adding a ninth is a visible change here, not a
// type switch someone forgets.
type DomainCall struct {
	// Explain, when non-nil, is the governed worker read that runs before
	// anything else. promotion.PreflightRequest accepts a baseline only as a
	// people.Explanation, so a caller's asserted facts can never become the
	// baseline; this field is how that ordering is expressed.
	Explain *people.ExplainWorkerStateRequest

	Promotion    *promotion.PreflightRequest
	Compensation *rewards.SimulateCompensationInput
	PayBand      *PayBandInputs

	// Drift is the cross-system comparison detect_drift runs.
	Drift *dataops.DetectDriftRequest
	// Repair is the shared input of create_repair_plan and simulate_repair.
	// The two intents read the same two sides and pin the same evidence; the
	// second one simulates the plan the first one would produce.
	Repair *RepairInputs
	// Transaction is the provenance reconstruction explain_transaction runs.
	Transaction *intelligence.ExplainTransactionRequest

	// Baseline is the exact input state the kernel preflight evaluates.
	Baseline intent.BaselineSnapshot
}

// RepairInputs is everything the repair capabilities need to read both sides,
// compare them, diagnose the disagreement and plan a correction.
//
// It carries no plan and no diagnosis: those are answers, and they are
// produced inside the governed capability that owns them. What it carries is
// the pinned coordinate the whole chain rests on, so that the plan and the
// simulation of it are demonstrably about the same comparison.
type RepairInputs struct {
	// PlanID is the derived, stable identity of the plan. It is an input
	// rather than a minted value so that planning twice over identical
	// evidence produces one plan, not two.
	PlanID  string
	Tenant  values.TenantId
	Subject values.EntityRef
	// Source is the observing system the external side is read from.
	Source string
	Fields []dataops.FieldID

	AsOfEffective values.LocalDate
	AsKnownAt     values.KnownAt
	EvaluatedAt   values.Instant
	IncludeClaims bool

	Authorization dataops.Authorization
	Freshness     dataops.FreshnessPolicy
	Approval      repair.ApprovalPolicy
	// AuthorityPolicyVersion pins the authority-by-field decision the safety
	// classification rested on.
	AuthorityPolicyVersion string
	// LocalSystem and ExternalSystem name the only two systems a step may
	// target.
	LocalSystem    string
	ExternalSystem string
	// PageLimit bounds one observation page.
	PageLimit int
}

// ResolveRequest is the question [DomainInputs] answers: which stored intent,
// under which definition, read by whom and for what.
//
// The principal and purpose are part of the question rather than ambient
// context because resolving a P1A intent means evaluating an authorization
// decision, and a resolver that had to reach into the context for the caller
// could be called without one.
type ResolveRequest struct {
	Instance   intent.Instance
	Definition intent.Definition
	Principal  *trust.Principal
	// Purpose is the resolved purpose of processing for this invocation.
	Purpose string
	// Relationships are trusted scope facts resolved by the calling surface.
	// The ordinary intent API leaves this empty. Journey inspection supplies a
	// single assignment-derived fact only for the current work-item owner.
	Relationships []authz.RelationshipFact
}

// DomainInputs resolves a [DomainCall] from a stored instance.
//
// It is a port because the source of those facts is a deployment decision. P1A
// has no worker projection of its own, so the shipped implementation
// ([NewFixtureInputs]) resolves against internal/domains/fixtures and the
// configured incumbent connector; a later phase replaces it with a real
// projection without changing this package's ordering.
type DomainInputs interface {
	Resolve(ctx context.Context, req ResolveRequest) (DomainCall, error)
}

// EvidenceRecord is one capability-gateway decision this cell recorded.
type EvidenceRecord struct {
	EvidenceID        string
	CapabilityID      string
	CapabilityVersion uint32
	SubjectRef        string
	Decision          string
	ReasonCode        string
	OccurredAt        time.Time
}
