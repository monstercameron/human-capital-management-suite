package timeclock

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/builders"
)

// Params are the typed, reviewed parameters of a time template. They are
// material to the definition and therefore to the compiled plan digest; a
// tenant overlay changes them only within the limits WF-EXT-026 sets.
type Params struct {
	TenantScope       string
	OrganizationScope string
	// ZoneID, TzdbVersion, CalendarRef and CalendarVersion pin the dataset
	// every WAIT resolves against.
	ZoneID          string
	TzdbVersion     string
	CalendarRef     string
	CalendarVersion string
	// MaxPunchesPerSession bounds the session's punch cycle (in, breaks,
	// transfers, out).
	MaxPunchesPerSession uint32
	// SessionCloseAfterSeconds bounds how long a session waits for its next
	// punch before the missing-out path starts.
	SessionCloseAfterSeconds uint64
	// PeriodCloseAfterSeconds bounds how long a period-level capture run
	// accepts lines, exceptions or entries.
	PeriodCloseAfterSeconds uint64
	// ReopenWindowSeconds bounds how long an accepted period listens for a
	// late session before it closes.
	ReopenWindowSeconds uint64
	// MaxPeriodPasses bounds the period's collect/approve cycle.
	MaxPeriodPasses uint32
}

// DefaultParams returns the product defaults.
func DefaultParams() Params {
	return Params{
		TenantScope: "tenant-template", OrganizationScope: "tenant-template/time",
		ZoneID: "America/Chicago", TzdbVersion: "2026a", CalendarRef: "us-federal", CalendarVersion: "2026.1",
		MaxPunchesPerSession:     48,
		SessionCloseAfterSeconds: 16 * 3600,
		PeriodCloseAfterSeconds:  16 * 24 * 3600,
		ReopenWindowSeconds:      14 * 24 * 3600,
		MaxPeriodPasses:          8,
	}
}

func (p Params) normalized() Params {
	d := DefaultParams()
	if p.TenantScope == "" {
		p.TenantScope = d.TenantScope
	}
	if p.OrganizationScope == "" {
		p.OrganizationScope = d.OrganizationScope
	}
	if p.ZoneID == "" {
		p.ZoneID = d.ZoneID
	}
	if p.TzdbVersion == "" {
		p.TzdbVersion = d.TzdbVersion
	}
	if p.CalendarRef == "" {
		p.CalendarRef = d.CalendarRef
	}
	if p.CalendarVersion == "" {
		p.CalendarVersion = d.CalendarVersion
	}
	if p.MaxPunchesPerSession == 0 {
		p.MaxPunchesPerSession = d.MaxPunchesPerSession
	}
	if p.SessionCloseAfterSeconds == 0 {
		p.SessionCloseAfterSeconds = d.SessionCloseAfterSeconds
	}
	if p.PeriodCloseAfterSeconds == 0 {
		p.PeriodCloseAfterSeconds = d.PeriodCloseAfterSeconds
	}
	if p.ReopenWindowSeconds == 0 {
		p.ReopenWindowSeconds = d.ReopenWindowSeconds
	}
	if p.MaxPeriodPasses == 0 {
		p.MaxPeriodPasses = d.MaxPeriodPasses
	}
	return p
}

// placeholderWakeInstant is the definition-time wake instant every WAIT
// carries until TimeExpr (WF-EXT-012) lets the node anchor on run data. The
// caller's timer factory substitutes the run's real anchor (scheduled end or
// period end plus grace) before the wake requirement is computed, exactly as
// the new-hire start-date WAIT does.
const placeholderWakeInstant = "1970-01-01T00:00:00Z"

// Signal sources admitted by the time templates. A subscription that accepts
// anyone is not a subscription.
const (
	SourceFirstPartyClock  = "hcmnext.time.clock"
	SourceKiosk            = "hcmnext.time.kiosk"
	SourceDeviceAdapter    = "hcmnext.integrations.time_device"
	SourcePunchImport      = "hcmnext.integrations.time_import"
	SourceTimesheetPortal  = "hcmnext.time.timesheet"
	SourceContractorPortal = "hcmnext.time.contractor_portal"
	SourceAgencyPortal     = "hcmnext.integrations.agency"
	SourceVMSConnector     = "hcmnext.integrations.vms"
	SourceDestination      = "hcmnext.integrations.time_destination"
)

// kit builds the nodes of one template with shared identity and governance.
type kit struct {
	workflowID     string
	purpose        string
	classification string
	manifest       string
	subjectBrand   string
	params         Params
}

func (k kit) schema(name string) workflow.SchemaRef {
	return workflow.SchemaRef{SchemaID: k.workflowID + "." + name + "/v1", Version: 1, ProtobufFullName: "hcmnext.workflows.time." + name}
}

func formSchema(form string) workflow.SchemaRef {
	return workflow.SchemaRef{SchemaID: form + "/v1", Version: 1, ProtobufFullName: "hcmnext.forms.v1.FormSubmission"}
}

func capSchema(id, slot string) workflow.SchemaRef {
	return workflow.SchemaRef{SchemaID: id + "." + slot + "/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}
}

func (k kit) subjectType() workflow.ValueType {
	return workflow.ValueType{Kind: workflow.KindString, Brand: k.subjectBrand}
}

func plainString() workflow.ValueType { return workflow.ValueType{Kind: workflow.KindString} }

func (k kit) inputs() []workflow.Field {
	return []workflow.Field{{Path: "subject_key", Type: k.subjectType()}}
}

func (k kit) workflowInputs() []workflow.Field {
	return []workflow.Field{
		{Path: "subject_key", Type: k.subjectType()},
		{Path: "profile_digest", Type: plainString()},
	}
}

func (k kit) mappings() []workflow.Mapping {
	return []workflow.Mapping{{Target: "subject_key", Source: builders.FromInput("subject_key")}}
}

func (k kit) governance(approvals []string, boundary workflow.RevalidationBoundary) workflow.NodeGovernance {
	return workflow.NodeGovernance{Purpose: k.purpose, Classification: k.classification,
		ApprovalRequirements: approvals, RevalidationBoundary: boundary, DataAccessManifestRef: k.manifest}
}

// capability builds a CAPABILITY node from the manifest table. A write takes
// an effect role, an idempotency key on the subject and PRE_EFFECT
// revalidation; a read takes PRE_EXECUTION.
func (k kit) capability(nodeID, capID string, role workflow.EffectRole, failure string, outputs ...workflow.Field) workflow.Node {
	m, _ := ManifestFor(capID)
	boundary := workflow.RevalidatePreExecution
	ref := &workflow.CapabilityRef{ID: capID, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{m.Scope}}
	if m.Effect.IsWrite() {
		boundary = workflow.RevalidatePreEffect
		ref.IdempotencyKeyMapping = "subject_key"
		ref.EffectBinding = nodeID + ".subject_key"
	} else {
		role = ""
	}
	return workflow.Node{
		ID: nodeID, Type: workflow.StepCapability, SafePointRequested: m.Effect.IsWrite(),
		InputSchema: capSchema(capID, "request"), OutputSchema: capSchema(capID, "response"),
		Inputs: k.inputs(), Outputs: outputs, InputMappings: k.mappings(),
		DeclaredEffect: m.Effect, EffectRole: role, Capability: ref, FailureRoute: failure,
		Governance: workflow.NodeGovernance{Purpose: k.purpose, Classification: k.classification,
			RequiredDecisions:    []workflow.GovernanceKind{workflow.GovernanceAuthZ, workflow.GovernanceLegal, workflow.GovernancePurpose, workflow.GovernanceRisk},
			RevalidationBoundary: boundary, DataAccessManifestRef: k.manifest},
	}
}

// decision builds a DECISION node whose routes carry distinct precedence in
// declaration order.
func (k kit) decision(nodeID, rule string, routes []string, inputs []workflow.Field, maps []workflow.Mapping) workflow.Node {
	declared := make([]workflow.DecisionRoute, 0, len(routes))
	for i, route := range routes {
		declared = append(declared, workflow.DecisionRoute{Key: route, Predicate: "route." + route, Precedence: (i + 1) * 10})
	}
	if inputs == nil {
		inputs, maps = k.inputs(), k.mappings()
	}
	return workflow.Node{
		ID: nodeID, Type: workflow.StepDecision,
		InputSchema: k.schema(nodeID + "Input"), OutputSchema: k.schema(nodeID + "Result"),
		Inputs: inputs, InputMappings: maps,
		Decision: &workflow.DecisionSpec{EvaluatorRef: "engines.rules.time", EvaluatorVersion: 1, RuleRef: rule,
			InputDigestProfile: "hcmnext.workflow.InputMappingSet/v1", Routes: declared},
		Governance: k.governance(nil, workflow.RevalidateNone),
	}
}

func (k kit) transform(nodeID, ref string, gap string) workflow.Node {
	n := workflow.Node{
		ID: nodeID, Type: workflow.StepTransform, DeclaredEffect: capability.EffectPure,
		InputSchema: k.schema(nodeID + "Input"), OutputSchema: k.schema(nodeID + "Result"),
		Inputs: k.inputs(), InputMappings: k.mappings(),
		Transform: &workflow.TransformSpec{TransformRef: ref, Version: 1, NormalizationProfile: "hcmnext.canonical." + nodeID + "/v1",
			OutputTaint: workflow.TaintDerived,
			Limits:      workflow.TransformLimits{MaxInputBytes: 256 * 1024, MaxOutputBytes: 64 * 1024, MaxSteps: 20_000}},
		Governance: k.governance(nil, workflow.RevalidateNone),
	}
	if gap != "" {
		n.Metadata = gapMetadata(gap)
	}
	return n
}

func (k kit) signal(nodeID, eventType, correlation string, closeAfter uint64, failure, gap string, sources ...string) workflow.Node {
	n := workflow.Node{
		ID: nodeID, Type: workflow.StepSignal, DeclaredEffect: capability.EffectPure,
		InputSchema: k.schema(nodeID + "Input"), OutputSchema: k.schema(nodeID + "Result"),
		Inputs: k.inputs(), InputMappings: k.mappings(),
		Signal: &workflow.SignalSpec{EventType: eventType, CorrelationKeyExpression: correlation,
			ExpectedSchemaRef: k.schema(nodeID + "Payload"), AcceptedSources: sources,
			Ordering: workflow.SignalOrderingMonotonicSequence, CloseAfterSeconds: closeAfter},
		FailureRoute: failure,
		Governance:   k.governance(nil, workflow.RevalidatePreExecution),
	}
	if gap != "" {
		n.Metadata = gapMetadata(gap)
	}
	return n
}

// wait builds a WAIT whose real anchor arrives through WF-EXT-012.
func (k kit) wait(nodeID, failure string) workflow.Node {
	return workflow.Node{
		ID: nodeID, Type: workflow.StepWait, SafePointRequested: true, DeclaredEffect: capability.EffectPure,
		InputSchema: k.schema(nodeID + "Input"), OutputSchema: k.schema(nodeID + "Result"),
		Inputs: k.inputs(), InputMappings: k.mappings(),
		Wait: &workflow.WaitSpec{WakeKind: workflow.WaitWakeAtInstant, WakeInstant: placeholderWakeInstant,
			ZoneID: k.params.ZoneID, ZoneTzdbVersion: k.params.TzdbVersion,
			CalendarRef: k.params.CalendarRef, CalendarVersion: k.params.CalendarVersion, ReferenceUpdatePolicy: "RECALCULATE"},
		FailureRoute: failure,
		Metadata:     gapMetadata(GapTimeExpr),
		Governance:   k.governance(nil, workflow.RevalidatePreExecution),
	}
}

func (k kit) approval(nodeID, requirement string) workflow.Node {
	return workflow.Node{
		ID: nodeID, Type: workflow.StepApproval, DeclaredEffect: capability.EffectPure,
		InputSchema: k.schema(nodeID + "Input"), OutputSchema: k.schema(nodeID + "Result"),
		Inputs: k.inputs(), InputMappings: k.mappings(),
		Governance: k.governance([]string{requirement}, workflow.RevalidatePreExecution),
	}
}

func (k kit) task(nodeID, form, assignee string) workflow.Node {
	return workflow.Node{
		ID: nodeID, Type: workflow.StepTask, DeclaredEffect: capability.EffectPure,
		InputSchema: k.schema(nodeID + "Input"), OutputSchema: formSchema(form),
		Inputs: k.inputs(), InputMappings: k.mappings(),
		Metadata:   map[string]string{"assignee": assignee, "form_ref": form},
		Governance: k.governance(nil, workflow.RevalidatePreExecution),
	}
}

func (k kit) observe(nodeID, capID, authority, exhaustion string) workflow.Node {
	m, _ := ManifestFor(capID)
	return workflow.Node{
		ID: nodeID, Type: workflow.StepObserve,
		InputSchema: capSchema(capID, "request"), OutputSchema: capSchema(capID, "response"),
		Inputs: k.inputs(), InputMappings: k.mappings(), DeclaredEffect: m.Effect,
		Capability: &workflow.CapabilityRef{ID: capID, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{m.Scope}},
		Observe: &workflow.ObserveSpec{EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: authority,
			ExpectedStateFields: []string{"subject_key"}, RequiredWatermarks: []string{authority + ".stream_head"},
			MaxAgeSeconds: 900, ComparisonProfile: "comparison." + nodeID + "/v1", RetryExhaustionRoute: exhaustion},
		Retry:        &workflow.RetryPolicy{MaxAttempts: 3, BackoffRef: "policy.retry.observation.bounded/v1"},
		FailureRoute: exhaustion,
		Governance: workflow.NodeGovernance{Purpose: k.purpose, Classification: k.classification,
			RequiredDecisions:    []workflow.GovernanceKind{workflow.GovernanceAuthZ, workflow.GovernanceLegal, workflow.GovernancePurpose, workflow.GovernanceRisk},
			RevalidationBoundary: workflow.RevalidatePreExecution, DataAccessManifestRef: k.manifest},
	}
}

// endOpts are the optional terminal facts one END declares.
type endOpts struct {
	receipt     string
	repair      string
	outstanding []string
	obligations []string
}

func (k kit) end(nodeID, code string, status workflow.RuntimeStatus, dims map[string]string, opts endOpts) workflow.Node {
	end := &workflow.EndSpec{TerminalCode: code, RuntimeStatus: status, CompletionMapping: dims,
		OutstandingObligationRefs: opts.outstanding, CommitReceiptRef: opts.receipt}
	if opts.repair != "" {
		end.RepairRefs = []string{opts.repair}
	}
	gov := k.governance(nil, workflow.RevalidatePreClosure)
	gov.ObligationRefs = opts.obligations
	return workflow.Node{
		ID: nodeID, Type: workflow.StepEnd, DeclaredEffect: capability.EffectPure,
		InputSchema: k.schema(nodeID + "Input"), OutputSchema: k.schema(nodeID + "Result"),
		Inputs:        builders.TerminalInputs("subject_key", k.subjectBrand),
		InputMappings: builders.TerminalMappings("subject_key", code),
		End:           end, Governance: gov,
	}
}

// Completion tuples the templates reuse.
func dimsSubmittedPending() map[string]string {
	return builders.Completion("SUBMITTED", "COMMITTED", "COMPLETED", "CONSISTENT", "PENDING")
}
func dimsClosed() map[string]string {
	return builders.Completion("CLOSED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED")
}
func dimsRejected() map[string]string {
	return builders.Completion("REJECTED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE")
}
func dimsHeld() map[string]string {
	return builders.Completion("REJECTED", "COMMITTED", "NOT_ACHIEVED", "CONSISTENT", "PENDING")
}
func dimsCancelled() map[string]string {
	return builders.Completion("CANCELLED", "NOT_PLANNED", "NOT_ACHIEVED", "NOT_APPLICABLE", "NOT_APPLICABLE")
}
func dimsBlocked() map[string]string {
	return builders.Completion("SUBMITTED", "BLOCKED", "UNKNOWN", "PENDING_OBSERVATION", "PENDING")
}
func dimsRepair() map[string]string {
	return builders.Completion("SUBMITTED", "REPAIR_REQUIRED", "UNKNOWN", "DEGRADED", "PENDING")
}
func dimsSuperseded() map[string]string {
	return builders.Completion("SUPERSEDED", "COMMITTED", "COMPLETED", "CONSISTENT", "PENDING")
}

// edge helpers.
func edge(from, to string, route string) workflow.Edge {
	return workflow.Edge{From: from, To: to, RouteKey: route}
}

func capabilityEdges(from, success, failure string) []workflow.Edge {
	return []workflow.Edge{
		edge(from, success, string(workflow.OutcomeSucceeded)),
		edge(from, failure, string(workflow.OutcomeRejected)),
		edge(from, failure, string(workflow.OutcomeUnknown)),
		edge(from, failure, string(workflow.OutcomeAmbiguous)),
	}
}

func observeEdges(from, pass, degraded string) []workflow.Edge {
	return []workflow.Edge{
		edge(from, pass, string(workflow.OutcomePass)),
		edge(from, degraded, string(workflow.OutcomeFail)),
		edge(from, degraded, string(workflow.OutcomePartial)),
		edge(from, degraded, string(workflow.OutcomeUnknown)),
	}
}

func routesTo(from, to string, routes ...string) []workflow.Edge {
	out := make([]workflow.Edge, 0, len(routes))
	for _, r := range routes {
		out = append(out, edge(from, to, r))
	}
	return out
}

func approvalRequirement(id, resolver string, p Params) workflow.ApprovalRequirement {
	return workflow.ApprovalRequirement{ID: id, ResolverExpression: resolver, Scope: p.OrganizationScope,
		Quorum: 1, SeparationOfDuties: true, EffectiveAsOfPolicy: "PROPOSAL_DIGEST_BOUND"}
}

func obligation(id, action, party string) workflow.ObligationRequirement {
	return workflow.ObligationRequirement{ID: id, Authority: "customer.policy.time", InsertionPoint: workflow.InsertClosure,
		RequiredAction: action, ResponsibleParty: party, SatisfactionCondition: action + " is recorded against the terminal",
		SourceVersion: id + "/v1", ReevaluationPolicy: workflow.ReevalPin, Mandatory: true}
}

func (k kit) definition(version uint32, name, intentType, start string, nodes []workflow.Node, edges []workflow.Edge) workflow.Definition {
	return workflow.Definition{
		WorkflowID: k.workflowID, Version: version, Name: name, IntentType: intentType,
		InputSchema: k.schema("Input"), OutputSchema: k.schema("Result"), VariablesSchema: k.schema("Variables"),
		Inputs:      k.workflowInputs(),
		Outputs:     []workflow.Field{{Path: "subject_key", Type: k.subjectType()}},
		TenantScope: k.params.TenantScope, OrganizationScope: k.params.OrganizationScope, RiskClass: "HIGH",
		DeclaredModes: []workflow.ExecutionMode{workflow.ModeExecute}, TerminalProfile: workflow.TerminalProfileExecute,
		StartNodeID: start, Nodes: nodes, Edges: edges,
		FailurePolicyRef:      "policy.workflow.failure.time/v1",
		CancellationPolicyRef: "policy.workflow.cancellation.time/v1",
		MigrationPolicyRef:    "policy.workflow.migration.pinned/v1",
		RetentionPolicyRef:    "policy.workflow.retention.time-records/v1",
	}
}
