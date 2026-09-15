package workflow

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// SchemaRef names one versioned schema by descriptor identity. It mirrors
// [capability.SchemaRef]; a workflow keeps its own value type so a definition
// can be authored, loaded and diffed without the capability registry present.
type SchemaRef struct {
	SchemaID         string `json:"schema_id"`
	Version          uint32 `json:"version"`
	ProtobufFullName string `json:"protobuf_full_name"`
}

// Valid reports whether every component of the reference is present.
func (s SchemaRef) Valid() bool {
	return s.SchemaID != "" && s.Version >= 1 && s.ProtobufFullName != ""
}

func (s SchemaRef) String() string { return fmt.Sprintf("%s/v%d", s.SchemaID, s.Version) }

// matchesCapabilitySchema reports whether s names exactly the capability
// schema c. A node binding a capability must name the capability's own
// request/response schema version; naming a different version is schema drift,
// not a convenience.
func (s SchemaRef) matchesCapabilitySchema(c capability.SchemaRef) bool {
	return s.SchemaID == c.SchemaID && s.Version == c.Version && s.ProtobufFullName == c.ProtobufFullName
}

// SourceKind names where a mapped value comes from.
type SourceKind string

// The declared mapping source kinds. There is no ambient source: a node reads
// the workflow input, a declared predecessor's declared output, a declared
// context path or a literal constant, and nothing else.
const (
	SourceWorkflowInput SourceKind = "WORKFLOW_INPUT"
	SourceNodeOutput    SourceKind = "NODE_OUTPUT"
	SourceContext       SourceKind = "CONTEXT"
	SourceConstant      SourceKind = "CONSTANT"
)

// Source is the producing side of one input mapping.
type Source struct {
	Kind SourceKind `json:"kind"`
	// NodeID names the producing node when Kind is NODE_OUTPUT.
	NodeID string `json:"node_id,omitempty"`
	// ContextKind names the context artifact when Kind is CONTEXT.
	ContextKind string `json:"context_kind,omitempty"`
	// Path is the field path within the producing surface. The wildcard "*"
	// is rejected: a node binds declared typed fields, never a whole
	// unrestricted result.
	Path string `json:"path,omitempty"`
	// Type is the declared type for CONTEXT and CONSTANT sources, whose types
	// are not derivable from another node's declaration.
	Type ValueType `json:"type,omitempty"`
	// Constant carries the canonical text of a CONSTANT source.
	Constant string `json:"constant,omitempty"`
}

// Mapping binds one declared input field of a node to one source.
type Mapping struct {
	Target string `json:"target"`
	Source Source `json:"source"`
}

// ContextRequirement declares the minimum-necessary context a node reads. A
// read outside this declaration fails; the engine never hands a node the whole
// worker.
type ContextRequirement struct {
	Kind                  string   `json:"kind"`
	FieldPaths            []string `json:"field_paths"`
	Purpose               string   `json:"purpose"`
	MaximumClassification string   `json:"maximum_classification"`
	MaxAgeSeconds         uint64   `json:"max_age_seconds"`
	RequiredWatermarks    []string `json:"required_watermarks,omitempty"`
	// Pinned marks a context snapshot pinned at an earlier evaluation point.
	// A DECISION may only read pinned context; an unpinned read is a hidden
	// current-state read.
	Pinned bool `json:"pinned,omitempty"`
	// MissingBehavior declares what a missing, masked or stale value means.
	// It is never "absent, zero or false".
	MissingBehavior string `json:"missing_behavior"`
}

// Declared missing-context behaviors.
const (
	MissingFail    = "FAIL"
	MissingUnknown = "UNKNOWN"
)

// CapabilityRef binds one exact governed capability version to a node.
type CapabilityRef struct {
	ID      string `json:"id"`
	Version uint32 `json:"version"`
	// OperationMode is the mode this invocation runs in.
	OperationMode ExecutionMode `json:"operation_mode"`
	// AuthorityScopes are the scopes the workflow's authority already carries.
	// They must cover the capability's declared AuthZ scope.
	AuthorityScopes []string `json:"authority_scopes"`
	// IdempotencyKeyMapping names the input field path that forms the stable
	// effect identity. It is required for any invocation that can mutate.
	IdempotencyKeyMapping string `json:"idempotency_key_mapping,omitempty"`
	// EffectBinding names the logical effect namespace a mutation belongs to.
	EffectBinding string `json:"effect_binding,omitempty"`
	// ExpectedVersions pins the baseline versions the invocation asserts.
	ExpectedVersions []string `json:"expected_versions,omitempty"`
}

// Key returns the capability identity this reference resolves.
func (c CapabilityRef) Key() capability.Key {
	return capability.Key{ID: c.ID, Version: c.Version}
}

// DecisionRoute is one explicitly declared DECISION outcome.
type DecisionRoute struct {
	Key string `json:"key"`
	// Predicate is the published predicate identity the evaluator applies. It
	// is a reference, not an expression this package interprets.
	Predicate string `json:"predicate"`
	// Precedence orders routes whose predicates may both hold. Routes that
	// share a predicate must declare distinct precedence.
	Precedence int `json:"precedence"`
}

// DecisionSpec configures a DECISION node. A DECISION selects exactly one
// typed route from already-available deterministic input; it calls no agent,
// queries no mutable data and mutates nothing.
type DecisionSpec struct {
	// EvaluatorRef and EvaluatorVersion identify the deterministic evaluator
	// recorded with every evaluation.
	EvaluatorRef     string `json:"evaluator_ref"`
	EvaluatorVersion uint32 `json:"evaluator_version"`
	// RuleRef names a published decision table or expression. RULE is not a
	// step type: it is a DECISION carrying this reference.
	RuleRef string `json:"rule_ref,omitempty"`
	// RuleVersion pins the exact published version of RuleRef. When the
	// compiler has a [ReferenceResolver], a rule reference without it is
	// unresolved; declaring it without a resolver is refused (WF-COMP-007).
	RuleVersion string `json:"rule_version,omitempty"`
	// InputDigestProfile names the canonical profile under which the pinned
	// input snapshot is digested.
	InputDigestProfile string          `json:"input_digest_profile"`
	Routes             []DecisionRoute `json:"routes"`
	// DefaultRoute is explicit and never positional.
	DefaultRoute string `json:"default_route,omitempty"`
}

// TaintLevel classifies the trust of a value flowing through a TRANSFORM.
type TaintLevel string

// The declared taint levels.
const (
	TaintTrusted TaintLevel = "TRUSTED"
	TaintDerived TaintLevel = "DERIVED"
	TaintTainted TaintLevel = "TAINTED"
)

// TaintInput is one input's declared taint manifest.
type TaintInput struct {
	Source string     `json:"source"`
	Level  TaintLevel `json:"level"`
}

// TransformLookup is a reference-data lookup a transform performs. Every
// lookup is pinned: an unpinned lookup makes a "deterministic" transform
// depend on whatever the reference table said today.
type TransformLookup struct {
	Ref            string `json:"ref"`
	SnapshotDigest string `json:"snapshot_digest"`
}

// TransformLimits are the declared resource bounds of one transform.
type TransformLimits struct {
	MaxInputBytes  uint64 `json:"max_input_bytes"`
	MaxOutputBytes uint64 `json:"max_output_bytes"`
	MaxSteps       uint64 `json:"max_steps"`
}

// Compiler ceilings for transform resource bounds. A definition may declare
// tighter bounds; it may never declare looser ones.
const (
	MaxTransformInputBytes  uint64 = 1 << 20
	MaxTransformOutputBytes uint64 = 1 << 20
	MaxTransformSteps       uint64 = 100_000
)

// TransformSpec configures a TRANSFORM node: a versioned deterministic,
// side-effect-free data mapping. The mapping itself is executed by the
// constrained transformation engine; this package binds and proves it.
type TransformSpec struct {
	TransformRef string `json:"transform_ref"`
	Version      uint32 `json:"version"`
	// InlineCode must be empty. The field exists so a definition carrying
	// arbitrary customer code is rejected with a named diagnostic rather than
	// silently accepted by a loader that ignored the key.
	InlineCode string `json:"inline_code,omitempty"`
	// UsesClock, UsesRandom and UsesNetwork must all be false: a transform
	// that reads the clock, randomness or the network is not deterministic
	// and cannot be replayed.
	UsesClock            bool              `json:"uses_clock,omitempty"`
	UsesRandom           bool              `json:"uses_random,omitempty"`
	UsesNetwork          bool              `json:"uses_network,omitempty"`
	Lookups              []TransformLookup `json:"lookups,omitempty"`
	NormalizationProfile string            `json:"normalization_profile"`
	InputTaint           []TaintInput      `json:"input_taint,omitempty"`
	// OutputTaint is the taint level the transform claims for its output.
	OutputTaint TaintLevel `json:"output_taint"`
	// SanitizerReceiptRef is required to downgrade TAINTED input.
	SanitizerReceiptRef string          `json:"sanitizer_receipt_ref,omitempty"`
	Limits              TransformLimits `json:"limits"`
}

// ObservationEvidenceKind classifies what an OBSERVE actually read.
type ObservationEvidenceKind string

// The declared observation evidence kinds. Only an authoritative read is an
// observation; a provider's acceptance of a submission is not.
const (
	EvidenceAuthoritativeRead ObservationEvidenceKind = "AUTHORITATIVE_READ"
	EvidenceSubmissionReceipt ObservationEvidenceKind = "SUBMISSION_RECEIPT"
	EvidenceTransportAck      ObservationEvidenceKind = "TRANSPORT_ACK"
)

// ObserveSpec configures an OBSERVE node.
type ObserveSpec struct {
	EvidenceKind ObservationEvidenceKind `json:"evidence_kind"`
	// SourceAuthority names the authority whose state counts as observed.
	SourceAuthority string `json:"source_authority"`
	// ExpectedStateFields are the input field paths carrying the expected
	// state the observation reconciles against.
	ExpectedStateFields []string `json:"expected_state_fields"`
	RequiredWatermarks  []string `json:"required_watermarks"`
	MaxAgeSeconds       uint64   `json:"max_age_seconds"`
	ComparisonProfile   string   `json:"comparison_profile"`
	// RetryExhaustionRoute names the node reached when bounded retry is
	// exhausted. It must lead to a degraded or repair terminal, never to a
	// completion terminal.
	RetryExhaustionRoute string `json:"retry_exhaustion_route,omitempty"`
}

// WaitWakeKind names which shape of wake condition a WAIT node declares. The
// three kinds mirror internal/workflow/steps/wait.WakeKind's wire vocabulary
// exactly, so a compiled WAIT node translates losslessly into that package's
// own typed view.
type WaitWakeKind string

// The declared wake-condition kinds.
const (
	WaitWakeAtInstant       WaitWakeKind = "AT_INSTANT"
	WaitWakeAtLocalDate     WaitWakeKind = "AT_LOCAL_DATE"
	WaitWakeAtLocalDateTime WaitWakeKind = "AT_LOCAL_DATETIME"
)

// Valid reports whether k names a declared wake-condition kind.
func (k WaitWakeKind) Valid() bool {
	switch k {
	case WaitWakeAtInstant, WaitWakeAtLocalDate, WaitWakeAtLocalDateTime:
		return true
	default:
		return false
	}
}

// WaitSpec configures a WAIT node: a durable suspension until a time or
// calendar condition (planning/workflows/_engine/step-types.md §5). The
// scheduler owns the timer; this package only binds and proves the wake
// condition, its DST disambiguation policy and the dataset reference-update
// policy that governs it — internal/workflow/steps/wait implements the
// actual pure timer resolution against these fields.
//
// A WAIT node's graph-level outcome routes remain the step type's fixed
// conformance outcomes (SUCCEEDED, LATE, CANCELLED; see [ConformanceFor]).
// internal/workflow/steps/wait's own four-way resolution vocabulary (FIRED,
// TIMER_REVIEW_REQUIRED, SUPERSEDED, CANCELLED) is what that package's
// Resolution.ToNodeOutcome maps onto those routes, an Await marker or a
// Failed attempt: FIRED and CANCELLED complete the node (on SUCCEEDED and
// CANCELLED respectively), TIMER_REVIEW_REQUIRED takes the node's declared
// FailureRoute, and SUPERSEDED leaves the node waiting on a fresh
// requirement rather than taking any route.
type WaitSpec struct {
	// WakeKind names which shape of wake condition this node declares.
	WakeKind WaitWakeKind `json:"wake_kind"`

	// WakeInstant is the canonical RFC 3339 UTC text of a fixed wake instant.
	// Set only when WakeKind is AT_INSTANT; mutually exclusive with
	// WakeLocalDate.
	WakeInstant string `json:"wake_instant,omitempty"`
	// WakeLocalDate is the canonical YYYY-MM-DD text of the business local
	// date this node wakes on. Set when WakeKind is AT_LOCAL_DATE or
	// AT_LOCAL_DATETIME.
	WakeLocalDate string `json:"wake_local_date,omitempty"`
	// WakeLocalTime is the canonical HH:MM:SS[.fffffffff] text of the local
	// wall-clock time. Set only when WakeKind is AT_LOCAL_DATETIME; an
	// AT_LOCAL_DATE condition wakes at the start of the local day.
	WakeLocalTime string `json:"wake_local_time,omitempty"`

	// Disambiguation names the DST gap/fold policy: REJECT_GAP, EARLIER,
	// LATER or EXPLICIT_OFFSET. Required whenever WakeKind is not AT_INSTANT.
	Disambiguation string `json:"disambiguation,omitempty"`

	// ZoneID and ZoneTzdbVersion name the IANA zone and tzdb release the wake
	// condition resolves against. Always required: even a fixed instant
	// preserves the calendar/tzdb identity in force when it was minted, so a
	// later replay can defend it.
	ZoneID          string `json:"zone_id"`
	ZoneTzdbVersion string `json:"zone_tzdb_version"`

	// CalendarRef and CalendarVersion name the business calendar dataset this
	// wake condition is pinned against.
	CalendarRef     string `json:"calendar_ref"`
	CalendarVersion string `json:"calendar_version"`

	// ReferenceUpdatePolicy declares what happens to an already-computed
	// deadline when the timezone/calendar dataset behind it is republished:
	// PIN, RECALCULATE or REVIEW_REQUIRED
	// (internal/kernel/values.ReferenceUpdatePolicy).
	ReferenceUpdatePolicy string `json:"reference_update_policy"`
}

// SignalOrdering names how a SIGNAL node expects successive signals on the
// same correlation to be numbered. It mirrors
// internal/workflow/steps/signal.OrderingExpectation's wire vocabulary.
type SignalOrdering string

// The declared ordering expectations.
const (
	SignalOrderingNone              SignalOrdering = "NONE"
	SignalOrderingMonotonicSequence SignalOrdering = "MONOTONIC_SEQUENCE"
)

// Valid reports whether o names a declared ordering expectation.
func (o SignalOrdering) Valid() bool {
	switch o {
	case SignalOrderingNone, SignalOrderingMonotonicSequence:
		return true
	default:
		return false
	}
}

// SignalSpec configures a SIGNAL node: a durable suspension until a
// correlated external event is accepted (planning/workflows/_engine/
// step-types.md §6). internal/workflow/steps/signal implements the actual
// pure acceptance decision against these fields; this package only binds and
// proves them.
//
// A SIGNAL node's graph-level outcome routes remain the step type's fixed
// conformance outcomes (SUCCEEDED, TIMED_OUT, CANCELLED; see
// [ConformanceFor]). internal/workflow/steps/signal's own Accept statuses map
// onto exactly those routes (see signal.Result.ToNodeOutcome): ACCEPTED and
// DUPLICATE_SAME_BYTES complete the node on SUCCEEDED, REFUSED_LATE
// completes it on TIMED_OUT, and every other refusal — an unmatched,
// wrong-source, wrong-schema or forged signal — takes the node's declared
// FailureRoute for security/operational review rather than a business
// outcome route.
type SignalSpec struct {
	// EventType names the domain event this node correlates against.
	EventType string `json:"event_type"`
	// CorrelationKeyExpression names the field path a runtime evaluates
	// against the workflow instance to produce the correlation value an
	// inbound signal must carry.
	CorrelationKeyExpression string `json:"correlation_key_expression"`
	// ExpectedSchemaRef names the versioned schema an accepted signal's
	// payload must conform to.
	ExpectedSchemaRef SchemaRef `json:"expected_schema_ref"`
	// AcceptedSources is the allowlist of sources permitted to satisfy this
	// subscription. A subscription that accepts anyone is not a subscription.
	AcceptedSources []string `json:"accepted_sources"`
	// Ordering declares the sequencing this node expects among signals on the
	// same correlation.
	Ordering SignalOrdering `json:"ordering"`
	// CloseAfterSeconds, when non-zero, is the duration after the node is
	// entered at which the subscription closes and a later signal is late.
	// Zero means the subscription never closes on its own.
	CloseAfterSeconds uint64 `json:"close_after_seconds,omitempty"`
}

// RuntimeStatus is the workflow instance's own status. It sits beside the
// intent's five dimensions and never substitutes for them.
type RuntimeStatus string

// The declared terminal runtime statuses.
const (
	RuntimeCompleted      RuntimeStatus = "COMPLETED"
	RuntimeCancelled      RuntimeStatus = "CANCELLED"
	RuntimeBlocked        RuntimeStatus = "BLOCKED"
	RuntimeRepairRequired RuntimeStatus = "REPAIR_REQUIRED"
	RuntimeQuarantined    RuntimeStatus = "QUARANTINED"
	RuntimeSuperseded     RuntimeStatus = "SUPERSEDED"
)

// Valid reports whether s names a declared terminal runtime status.
func (s RuntimeStatus) Valid() bool {
	switch s {
	case RuntimeCompleted, RuntimeCancelled, RuntimeBlocked, RuntimeRepairRequired,
		RuntimeQuarantined, RuntimeSuperseded:
		return true
	default:
		return false
	}
}

// EndSpec configures an END node: a typed terminal runtime result that never
// equates runtime completion with business success.
type EndSpec struct {
	TerminalCode  string        `json:"terminal_code"`
	RuntimeStatus RuntimeStatus `json:"runtime_status"`
	// CompletionMapping records the intent's five lifecycle dimensions by
	// name. Exactly five keys are legal; a sixth key is rejected rather than
	// stored, which is how "END never writes a sixth dimension" is enforced
	// instead of merely documented.
	CompletionMapping         map[string]string `json:"completion_mapping"`
	OutstandingObligationRefs []string          `json:"outstanding_obligation_refs,omitempty"`
	RepairRefs                []string          `json:"repair_refs,omitempty"`
	IncidentRefs              []string          `json:"incident_refs,omitempty"`
	// CommitReceiptRef is the receipt a COMMITTED execution produced.
	CommitReceiptRef string `json:"commit_receipt_ref,omitempty"`
	// ApprovalRequired mirrors the definition's declaration for the terminal
	// legality rules.
	ApprovalRequired bool `json:"approval_required,omitempty"`
	// ApprovedMaterialDigest is the material proposal digest a bound approval
	// carries, when the terminal claims APPROVED.
	ApprovedMaterialDigest string `json:"approved_material_digest,omitempty"`
	// ClosurePolicyPermitsOpenRepair mirrors the definition's closure policy.
	ClosurePolicyPermitsOpenRepair bool `json:"closure_policy_permits_open_repair,omitempty"`
}

// GovernanceKind names one composed governance subdecision a node requires.
type GovernanceKind string

// The governance subdecisions the coordinator composes.
const (
	GovernanceAuthZ       GovernanceKind = "AUTHZ"
	GovernanceLegal       GovernanceKind = "LEGAL"
	GovernancePrivacy     GovernanceKind = "PRIVACY"
	GovernancePurpose     GovernanceKind = "PURPOSE"
	GovernanceRisk        GovernanceKind = "RISK"
	GovernanceEntitlement GovernanceKind = "ENTITLEMENT"
)

// RevalidationBoundary names when a node's governance decision must be
// re-evaluated. Proposal-time allow never implies execution-time allow.
type RevalidationBoundary string

// The declared revalidation boundaries.
const (
	RevalidateNone         RevalidationBoundary = "NONE"
	RevalidatePreExecution RevalidationBoundary = "PRE_EXECUTION"
	RevalidatePreEffect    RevalidationBoundary = "PRE_EFFECT"
	RevalidatePreClosure   RevalidationBoundary = "PRE_CLOSURE"
)

// NodeGovernance is the governance surface one node declares.
type NodeGovernance struct {
	Purpose               string               `json:"purpose"`
	Classification        string               `json:"classification"`
	RequiredDecisions     []GovernanceKind     `json:"required_decisions,omitempty"`
	ObligationRefs        []string             `json:"obligation_refs,omitempty"`
	ApprovalRequirements  []string             `json:"approval_requirements,omitempty"`
	RevalidationBoundary  RevalidationBoundary `json:"revalidation_boundary"`
	DataAccessManifestRef string               `json:"data_access_manifest_ref"`
	// OutputValidatorRef names the typed output validator that turns
	// derived/untrusted output into something an effect may consume. It is
	// mandatory on any path from an agent-eligible capability to an effect.
	OutputValidatorRef string `json:"output_validator_ref,omitempty"`
}

// ApprovalRequirement is one declared approval the workflow may require. The
// workflow declares the requirement and its scope; the approval service owns
// resolution, quorum and authority.
type ApprovalRequirement struct {
	ID                 string `json:"id"`
	ResolverExpression string `json:"resolver_expression"`
	// Scope is the organization scope the resolver may reach. It must be
	// within the workflow's own organization scope: a workflow cannot
	// escalate to an approver outside the scope it was authorized for.
	Scope               string `json:"scope"`
	Quorum              int    `json:"quorum"`
	SeparationOfDuties  bool   `json:"separation_of_duties"`
	EffectiveAsOfPolicy string `json:"effective_as_of_policy"`
}

// InsertionPoint names where an obligation is inserted or proved.
type InsertionPoint string

// The legal/obligation insertion points.
const (
	InsertDefinitionPublication InsertionPoint = "DEFINITION_PUBLICATION"
	InsertIntentPreflight       InsertionPoint = "INTENT_PREFLIGHT"
	InsertSimulation            InsertionPoint = "SIMULATION"
	InsertApproval              InsertionPoint = "APPROVAL"
	InsertWait                  InsertionPoint = "WAIT"
	InsertExecutionRevalidation InsertionPoint = "EXECUTION_REVALIDATION"
	InsertExternalEffect        InsertionPoint = "EXTERNAL_EFFECT"
	InsertClosure               InsertionPoint = "CLOSURE"
)

// Valid reports whether p names a declared insertion point.
func (p InsertionPoint) Valid() bool {
	switch p {
	case InsertDefinitionPublication, InsertIntentPreflight, InsertSimulation, InsertApproval,
		InsertWait, InsertExecutionRevalidation, InsertExternalEffect, InsertClosure:
		return true
	default:
		return false
	}
}

// ReevaluationPolicy declares how an obligation responds to a rule change.
type ReevaluationPolicy string

// The declared reevaluation policies. A rule change never silently rewrites a
// live instance.
const (
	ReevalPin           ReevaluationPolicy = "PIN"
	ReevalReevaluate    ReevaluationPolicy = "REEVALUATE"
	ReevalRequireReview ReevaluationPolicy = "REQUIRE_REVIEW"
	ReevalBlock         ReevaluationPolicy = "BLOCK"
)

// Valid reports whether p names a declared reevaluation policy.
func (p ReevaluationPolicy) Valid() bool {
	switch p {
	case ReevalPin, ReevalReevaluate, ReevalRequireReview, ReevalBlock:
		return true
	default:
		return false
	}
}

// ObligationRequirement is one obligation the compiled plan must be able to
// discharge. The workflow does not interpret law; it records what a legal or
// governance capability said and where the workflow proves it.
type ObligationRequirement struct {
	ID                    string             `json:"id"`
	Authority             string             `json:"authority"`
	InsertionPoint        InsertionPoint     `json:"insertion_point"`
	RequiredAction        string             `json:"required_action"`
	ResponsibleParty      string             `json:"responsible_party"`
	SatisfactionCondition string             `json:"satisfaction_condition"`
	SourceVersion         string             `json:"source_version"`
	ReevaluationPolicy    ReevaluationPolicy `json:"reevaluation_policy"`
	// Mandatory obligations cannot be collapsed into a generic success at END.
	Mandatory bool `json:"mandatory"`
}

// RetryPolicy is a node's declared bounded retry.
type RetryPolicy struct {
	MaxAttempts uint32 `json:"max_attempts"`
	BackoffRef  string `json:"backoff_ref"`
}

// CycleDeclaration bounds one declared cycle. An undeclared cycle, or a
// declared cycle with no iteration bound or no DECISION guard inside it, is a
// compile error: the runtime does not discover loops at execution time.
type CycleDeclaration struct {
	EntryNodeID   string `json:"entry_node_id"`
	GuardNodeID   string `json:"guard_node_id"`
	MaxIterations uint32 `json:"max_iterations"`
}

// Limits are the declared execution bounds of a definition.
type Limits struct {
	MaxFanOut      uint32             `json:"max_fan_out"`
	MaxDepth       uint32             `json:"max_depth"`
	MaxNodes       uint32             `json:"max_nodes"`
	DeclaredCycles []CycleDeclaration `json:"declared_cycles,omitempty"`
}

// Edge is one explicitly routed transition. There is no implicit first edge:
// every edge names the outcome route key it carries.
type Edge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	RouteKey string `json:"route_key"`
}

// Node is one step definition.
type Node struct {
	ID   string   `json:"id"`
	Type StepType `json:"type"`

	InputSchema  SchemaRef `json:"input_schema"`
	OutputSchema SchemaRef `json:"output_schema"`
	Inputs       []Field   `json:"inputs"`
	Outputs      []Field   `json:"outputs,omitempty"`

	InputMappings   []Mapping            `json:"input_mappings,omitempty"`
	RequiredContext []ContextRequirement `json:"required_context,omitempty"`

	Capability *CapabilityRef `json:"capability,omitempty"`
	Decision   *DecisionSpec  `json:"decision,omitempty"`
	Transform  *TransformSpec `json:"transform,omitempty"`
	Observe    *ObserveSpec   `json:"observe,omitempty"`
	End        *EndSpec       `json:"end,omitempty"`
	Wait       *WaitSpec      `json:"wait,omitempty"`
	Signal     *SignalSpec    `json:"signal,omitempty"`

	// DeclaredEffect is the author's claim about this node's side-effect
	// profile. Where a capability manifest disagrees, the manifest wins and
	// the disagreement is a compile error.
	DeclaredEffect capability.EffectClass `json:"declared_effect,omitempty"`

	// ResolverRef, TimeoutPolicy and CompensationRef bind published
	// resolver, timeout-policy and compensation versions. Each resolves
	// through the compiler's [ReferenceResolver]; a definition declaring one
	// cannot compile without that resolver (WF-COMP-007).
	ResolverRef     *VersionedRef `json:"resolver_ref,omitempty"`
	TimeoutPolicy   *VersionedRef `json:"timeout_policy,omitempty"`
	CompensationRef *VersionedRef `json:"compensation_ref,omitempty"`

	Retry *RetryPolicy `json:"retry,omitempty"`
	// FailureRoute names the node reached when the step fails outside its
	// declared outcome routes.
	FailureRoute string `json:"failure_route,omitempty"`
	// SafePointRequested records an author's request. The compiler decides
	// where safe points actually go; CHECKPOINT is not a step type.
	SafePointRequested bool              `json:"safe_point_requested,omitempty"`
	Governance         NodeGovernance    `json:"governance"`
	Metadata           map[string]string `json:"metadata,omitempty"`
}

// Definition is one draft workflow definition version. Publishing it produces
// an immutable [CompiledWorkflow]; changing anything produces a new version.
type Definition struct {
	WorkflowID string `json:"workflow_id"`
	Version    uint32 `json:"version"`
	Name       string `json:"name"`

	InputSchema     SchemaRef `json:"input_schema"`
	OutputSchema    SchemaRef `json:"output_schema"`
	VariablesSchema SchemaRef `json:"variables_schema"`
	Inputs          []Field   `json:"inputs"`
	Outputs         []Field   `json:"outputs"`

	TenantScope       string `json:"tenant_scope"`
	OrganizationScope string `json:"organization_scope"`
	RiskClass         string `json:"risk_class"`

	// DeclaredModes are the execution modes this definition claims to
	// support. Claiming SIMULATE while carrying an unsuppressable mutation is
	// a compile error, not a runtime surprise.
	DeclaredModes   []ExecutionMode `json:"declared_modes"`
	TerminalProfile TerminalProfile `json:"terminal_profile"`

	StartNodeID string `json:"start_node_id"`
	Nodes       []Node `json:"nodes"`
	Edges       []Edge `json:"edges"`

	ApprovalRequirements []ApprovalRequirement   `json:"approval_requirements,omitempty"`
	Obligations          []ObligationRequirement `json:"obligations,omitempty"`
	Limits               Limits                  `json:"limits"`

	FailurePolicyRef      string `json:"failure_policy_ref"`
	CancellationPolicyRef string `json:"cancellation_policy_ref"`
	MigrationPolicyRef    string `json:"migration_policy_ref"`
	RetentionPolicyRef    string `json:"retention_policy_ref"`
}

// declaresMode reports whether the definition claims mode.
func (d *Definition) declaresMode(mode ExecutionMode) bool {
	for _, m := range d.DeclaredModes {
		if m == mode {
			return true
		}
	}
	return false
}

// nodeIndex maps node id to node for the compilation passes.
func (d *Definition) nodeIndex() map[string]*Node {
	out := make(map[string]*Node, len(d.Nodes))
	for i := range d.Nodes {
		out[d.Nodes[i].ID] = &d.Nodes[i]
	}
	return out
}
