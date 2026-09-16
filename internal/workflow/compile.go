package workflow

import (
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// CompilerVersion identifies the compiler that produced a plan. It is part of
// the plan's material identity: the same definition compiled by a different
// compiler is a different plan, and a runtime pins both.
const CompilerVersion = "hcmnext.workflow.compiler/v1"

// CapabilityResolver resolves one exact capability version. *capability.Registry
// satisfies it, and a test can supply a fixed table without the bootstrap
// registry. The compiler never invokes a capability; it only reads manifests.
type CapabilityResolver interface {
	Lookup(key capability.Key) (capability.Record, bool)
}

// Options configures one compilation.
type Options struct {
	// Phase gates which primitives the compiler implements. The zero value is
	// PhaseP1A.
	Phase Phase
	// Capabilities resolves the capability versions the definition binds. A
	// nil resolver makes every capability reference unresolvable, which is a
	// diagnostic rather than a panic.
	Capabilities CapabilityResolver
	// References resolves schema, rule, resolver, timeout-policy and
	// compensation references against published registries (WF-COMP-007).
	// A nil resolver leaves schema and unversioned rule references as they
	// were before it existed, and refuses every reference kind only a
	// resolver can check with [CodeReferenceResolverRequired].
	References ReferenceResolver
	// CompilerVersion overrides [CompilerVersion] for tests that need to prove
	// the compiler identity is material to the digest.
	CompilerVersion string
}

func (o Options) phase() Phase {
	if o.Phase == "" {
		return PhaseP1A
	}
	return o.Phase
}

func (o Options) compilerVersion() string {
	if o.CompilerVersion == "" {
		return CompilerVersion
	}
	return o.CompilerVersion
}

// requiresZeroEffect reports whether this compilation refuses every write
// effect. P1A ships paid observation, preflight and simulation only, so a P1A
// plan must compile to zero effect; there is no flag that relaxes it.
func (o Options) requiresZeroEffect() bool { return o.phase() == PhaseP1A }

// CompiledMapping is one resolved, type-checked input binding.
type CompiledMapping struct {
	Target      string     `json:"target"`
	TargetType  ValueType  `json:"target_type"`
	SourceKind  SourceKind `json:"source_kind"`
	SourceNode  string     `json:"source_node,omitempty"`
	SourceCtx   string     `json:"source_context_kind,omitempty"`
	SourcePath  string     `json:"source_path,omitempty"`
	SourceType  ValueType  `json:"source_type"`
	Constant    string     `json:"constant,omitempty"`
	PinnedInput bool       `json:"pinned_input"`
}

// CompiledCapability is a node's resolved capability binding, including the
// manifest facts the compiler proved against.
type CompiledCapability struct {
	ID                    string                 `json:"id"`
	Version               uint32                 `json:"version"`
	Digest                string                 `json:"digest"`
	Status                capability.Status      `json:"status"`
	OwnerDomain           string                 `json:"owner_domain"`
	RequestSchema         SchemaRef              `json:"request_schema"`
	ResponseSchema        SchemaRef              `json:"response_schema"`
	EffectClass           capability.EffectClass `json:"effect_class"`
	AuthZScopeRef         string                 `json:"authz_scope_ref"`
	IdempotencyPolicyRef  string                 `json:"idempotency_policy_ref"`
	AgentEligible         bool                   `json:"agent_eligible"`
	OperationMode         ExecutionMode          `json:"operation_mode"`
	AuthorityScopes       []string               `json:"authority_scopes"`
	IdempotencyKeyMapping string                 `json:"idempotency_key_mapping,omitempty"`
	EffectBinding         string                 `json:"effect_binding,omitempty"`
	ExpectedVersions      []string               `json:"expected_versions,omitempty"`
}

// CompiledDecision is a resolved DECISION binding.
type CompiledDecision struct {
	EvaluatorRef       string          `json:"evaluator_ref"`
	EvaluatorVersion   uint32          `json:"evaluator_version"`
	RuleRef            string          `json:"rule_ref,omitempty"`
	InputDigestProfile string          `json:"input_digest_profile"`
	InputDigest        string          `json:"input_digest"`
	Routes             []DecisionRoute `json:"routes"`
	DefaultRoute       string          `json:"default_route,omitempty"`
	// Rule is the resolved published rule target, present when the plan was
	// compiled with a [ReferenceResolver] and the DECISION cites a rule.
	Rule *ResolvedReference `json:"rule,omitempty"`
}

// CompiledTransform is a resolved TRANSFORM binding with its taint lineage.
type CompiledTransform struct {
	TransformRef         string            `json:"transform_ref"`
	Version              uint32            `json:"version"`
	NormalizationProfile string            `json:"normalization_profile"`
	Lookups              []TransformLookup `json:"lookups,omitempty"`
	InputDigest          string            `json:"input_digest"`
	// Lineage is the ordered, deduplicated taint provenance of the output:
	// which declared inputs contributed and at what level.
	Lineage             []string        `json:"lineage"`
	OutputTaint         TaintLevel      `json:"output_taint"`
	SanitizerReceiptRef string          `json:"sanitizer_receipt_ref,omitempty"`
	Limits              TransformLimits `json:"limits"`
}

// CompiledObserve is a resolved OBSERVE binding.
type CompiledObserve struct {
	EvidenceKind         ObservationEvidenceKind `json:"evidence_kind"`
	SourceAuthority      string                  `json:"source_authority"`
	ExpectedStateFields  []string                `json:"expected_state_fields"`
	RequiredWatermarks   []string                `json:"required_watermarks"`
	MaxAgeSeconds        uint64                  `json:"max_age_seconds"`
	ComparisonProfile    string                  `json:"comparison_profile"`
	RetryExhaustionRoute string                  `json:"retry_exhaustion_route,omitempty"`
}

// CompiledWait is a resolved WAIT binding: the wake condition and dataset
// identity a future timer must carry, exactly as declared on [WaitSpec].
// internal/workflow/steps/wait.FromCompiled translates this into that
// package's own [wait.CompiledWaitNode] view.
type CompiledWait struct {
	WakeKind              WaitWakeKind `json:"wake_kind"`
	WakeInstant           string       `json:"wake_instant,omitempty"`
	WakeLocalDate         string       `json:"wake_local_date,omitempty"`
	WakeLocalTime         string       `json:"wake_local_time,omitempty"`
	Disambiguation        string       `json:"disambiguation,omitempty"`
	ZoneID                string       `json:"zone_id"`
	ZoneTzdbVersion       string       `json:"zone_tzdb_version"`
	CalendarRef           string       `json:"calendar_ref"`
	CalendarVersion       string       `json:"calendar_version"`
	ReferenceUpdatePolicy string       `json:"reference_update_policy"`
}

// CompiledSignal is a resolved SIGNAL binding: the correlation, schema and
// source identity a durable subscription must carry, exactly as declared on
// [SignalSpec]. internal/workflow/steps/signal.FromCompiled translates this,
// together with per-instance context, into that package's own
// [signal.SignalSubscription] view.
type CompiledSignal struct {
	EventType                string    `json:"event_type"`
	CorrelationKeyExpression string    `json:"correlation_key_expression"`
	ExpectedSchemaRef        SchemaRef `json:"expected_schema_ref"`
	AcceptedSources          []string  `json:"accepted_sources,omitempty"`
	Ordering                 string    `json:"ordering"`
	CloseAfterSeconds        uint64    `json:"close_after_seconds,omitempty"`
}

// CompiledGovernance is a node's resolved governance surface.
type CompiledGovernance struct {
	Purpose               string               `json:"purpose"`
	Classification        string               `json:"classification"`
	RequiredDecisions     []GovernanceKind     `json:"required_decisions"`
	ObligationRefs        []string             `json:"obligation_refs,omitempty"`
	ApprovalRequirements  []string             `json:"approval_requirements,omitempty"`
	RevalidationBoundary  RevalidationBoundary `json:"revalidation_boundary"`
	DataAccessManifestRef string               `json:"data_access_manifest_ref"`
	OutputValidatorRef    string               `json:"output_validator_ref,omitempty"`
}

// CompiledNode is one normalized node of an immutable plan.
type CompiledNode struct {
	ID           string    `json:"id"`
	Type         StepType  `json:"type"`
	Depth        uint32    `json:"depth"`
	SafePoint    bool      `json:"safe_point"`
	InputSchema  SchemaRef `json:"input_schema"`
	OutputSchema SchemaRef `json:"output_schema"`
	Inputs       []Field   `json:"inputs"`
	Outputs      []Field   `json:"outputs,omitempty"`

	Mappings        []CompiledMapping    `json:"mappings,omitempty"`
	RequiredContext []ContextRequirement `json:"required_context,omitempty"`
	Routes          []string             `json:"routes,omitempty"`

	Capability *CompiledCapability `json:"capability,omitempty"`
	Decision   *CompiledDecision   `json:"decision,omitempty"`
	Transform  *CompiledTransform  `json:"transform,omitempty"`
	Observe    *CompiledObserve    `json:"observe,omitempty"`
	Terminal   *Terminal           `json:"terminal,omitempty"`
	Wait       *CompiledWait       `json:"wait,omitempty"`
	Signal     *CompiledSignal     `json:"signal,omitempty"`

	EffectClass  capability.EffectClass `json:"effect_class"`
	EffectKey    string                 `json:"effect_key,omitempty"`
	EffectRole   EffectRole             `json:"effect_role,omitempty"` // WF-RUN-037; empty when nothing is mutated
	AllowedModes []ExecutionMode        `json:"allowed_modes"`
	Retry        *RetryPolicy           `json:"retry,omitempty"`
	FailureRoute string                 `json:"failure_route,omitempty"`
	Governance   CompiledGovernance     `json:"governance"`
	EvidenceRefs []string               `json:"evidence_refs"`

	// ResolverRef, TimeoutPolicy and CompensationRef are the resolved
	// published targets the node binds (WF-COMP-007).
	ResolverRef     *ResolvedReference `json:"resolver_ref,omitempty"`
	TimeoutPolicy   *ResolvedReference `json:"timeout_policy,omitempty"`
	CompensationRef *ResolvedReference `json:"compensation_ref,omitempty"`
}

// ReachabilityProof is the compiled evidence that the graph is sound: which
// nodes the start reaches, in what order, at what depth, which nodes are
// terminal and which edges close a declared cycle.
type ReachabilityProof struct {
	StartNodeID string            `json:"start_node_id"`
	Order       []string          `json:"order"`
	Depth       map[string]uint32 `json:"depth"`
	Terminals   []string          `json:"terminals"`
	BackEdges   []Edge            `json:"back_edges,omitempty"`
}

// EffectSummary is the compiled side-effect and idempotency analysis.
type EffectSummary struct {
	// ZeroEffect is true when no node can mutate anything anywhere.
	ZeroEffect bool `json:"zero_effect"`
	// NodesByClass lists node ids per declared effect class.
	NodesByClass map[string][]string `json:"nodes_by_class"`
	// EffectKeys are the logical effect identities the plan can produce.
	EffectKeys []string `json:"effect_keys,omitempty"`
	// IrreversibleNodes are the nodes whose effects cannot be undone.
	IrreversibleNodes []string `json:"irreversible_nodes,omitempty"`
	// AllowedModes are the execution modes every node in the plan supports.
	AllowedModes []ExecutionMode `json:"allowed_modes"`
	// NodesByRole lists write-effect node ids per WF-RUN-037 effect role. It
	// is absent for a plan with no write effect.
	NodesByRole map[string][]string `json:"nodes_by_role,omitempty"`
}

// GovernanceSummary is the compiled governance and obligation surface.
type GovernanceSummary struct {
	RequiredContexts     []ContextRequirement            `json:"required_contexts"`
	RequiredDecisions    []GovernanceKind                `json:"required_decisions"`
	ApprovalRequirements []ApprovalRequirement           `json:"approval_requirements,omitempty"`
	Obligations          []ObligationRequirement         `json:"obligations,omitempty"`
	InsertionPoints      map[string][]string             `json:"insertion_points,omitempty"`
	RevalidationPoints   map[string]RevalidationBoundary `json:"revalidation_points,omitempty"`
}

// CompiledWorkflow is the immutable normalized IR one publication produces.
//
// It is a value, not a handle: [Compile] deep-copies everything it reads, so a
// plan never aliases the draft definition it came from. Treat it as immutable
// — [CompiledWorkflow.Verify] recomputes the digest from the plan's own
// content, so a plan edited after compilation stops verifying.
type CompiledWorkflow struct {
	WorkflowID      string          `json:"workflow_id"`
	Version         uint32          `json:"version"`
	Name            string          `json:"name"`
	CompilerVersion string          `json:"compiler_version"`
	Phase           Phase           `json:"phase"`
	TerminalProfile TerminalProfile `json:"terminal_profile"`

	TenantScope       string `json:"tenant_scope"`
	OrganizationScope string `json:"organization_scope"`
	RiskClass         string `json:"risk_class"`

	InputSchema     SchemaRef `json:"input_schema"`
	OutputSchema    SchemaRef `json:"output_schema"`
	VariablesSchema SchemaRef `json:"variables_schema"`
	Inputs          []Field   `json:"inputs"`
	Outputs         []Field   `json:"outputs"`

	StartNodeID string         `json:"start_node_id"`
	Nodes       []CompiledNode `json:"nodes"`
	Edges       []Edge         `json:"edges"`
	Limits      Limits         `json:"limits"`

	Reachability ReachabilityProof `json:"reachability"`
	Effects      EffectSummary     `json:"effects"`
	Governance   GovernanceSummary `json:"governance"`
	Terminals    []Terminal        `json:"terminals"`

	// Concurrency is WF-COMP-004's branch/join/atomic-region analysis. It is
	// nil, and absent from the canonical bytes, for a plan that declares no
	// PARALLEL node and holds no unobserved external mutation -- see
	// [ConcurrencySummary] for why that absence is deliberate.
	Concurrency *ConcurrencySummary `json:"concurrency,omitempty"`

	FailurePolicyRef      string `json:"failure_policy_ref"`
	CancellationPolicyRef string `json:"cancellation_policy_ref"`
	MigrationPolicyRef    string `json:"migration_policy_ref"`
	RetentionPolicyRef    string `json:"retention_policy_ref"`

	// References is every published target the plan resolved -- schemas,
	// rules, resolvers, timeout policies, compensations -- with its version,
	// digest and status, sorted and deduplicated. Because it is part of the
	// canonical bytes, the plan digest pins every target (WF-COMP-007). It is
	// absent for a plan compiled without a [ReferenceResolver].
	References []ResolvedReference `json:"references,omitempty"`

	digest string
}

// Digest is the plan's content digest: the identity a runtime pins, an
// approval binds and a version comparison uses.
func (p *CompiledWorkflow) Digest() string { return p.digest }

// Node returns one compiled node by id.
func (p *CompiledWorkflow) Node(id string) (CompiledNode, bool) {
	for _, n := range p.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return CompiledNode{}, false
}

// Verify recomputes the digest from the plan's current content and reports
// whether it still matches the digest minted at compilation.
func (p *CompiledWorkflow) Verify() error {
	got := computePlanDigest(p)
	if got != p.digest {
		return Error{
			Code:     CodeInvalidDefinition,
			Location: Location{Ref: p.WorkflowID},
			Detail:   "compiled plan content no longer matches its digest",
		}
	}
	return nil
}

// Compile turns a draft definition into an immutable compiled plan, or reports
// every diagnostic that prevented it. A plan is produced only when the
// diagnostic set is empty: there is no partially valid publication.
func Compile(def Definition, opts Options) (*CompiledWorkflow, error) {
	c := &collector{}

	validateShape(&def, opts, c)
	records := resolveCapabilities(&def, opts, c)
	refs := resolveReferences(&def, opts, c)
	g := analyzeGraph(&def, c)
	if g.sound {
		checkMappings(&def, g, c)
	}
	checkSteps(&def, g, records, c)
	effects := analyzeEffects(&def, g, records, opts, c)
	effects.NodesByRole = analyzeEffectRoles(g, records, c)
	governance := analyzeGovernance(&def, g, records, c)
	concurrency := analyzeConcurrency(&def, g, records, c)

	terminals := map[string]*Terminal{}
	for i := range def.Nodes {
		n := &def.Nodes[i]
		if n.Type != StepEnd {
			continue
		}
		if t := checkEnd(&def, n, g.degradedEntry[n.ID], c); t != nil {
			terminals[n.ID] = t
		}
	}

	if d := c.result(); d != nil {
		return nil, d
	}

	return normalize(&def, opts, g, records, refs, effects, governance, concurrency, terminals), nil
}

// normalize builds the immutable plan from the proved definition.
func normalize(
	def *Definition,
	opts Options,
	g *graph,
	records map[string]capability.Record,
	refs resolvedReferences,
	effects EffectSummary,
	governance GovernanceSummary,
	concurrency *ConcurrencySummary,
	terminals map[string]*Terminal,
) *CompiledWorkflow {
	plan := &CompiledWorkflow{
		WorkflowID:            def.WorkflowID,
		Version:               def.Version,
		Name:                  def.Name,
		CompilerVersion:       opts.compilerVersion(),
		Phase:                 opts.phase(),
		TerminalProfile:       def.TerminalProfile,
		TenantScope:           def.TenantScope,
		OrganizationScope:     def.OrganizationScope,
		RiskClass:             def.RiskClass,
		InputSchema:           def.InputSchema,
		OutputSchema:          def.OutputSchema,
		VariablesSchema:       def.VariablesSchema,
		Inputs:                cloneFields(def.Inputs),
		Outputs:               cloneFields(def.Outputs),
		StartNodeID:           def.StartNodeID,
		Limits:                cloneLimits(def.Limits),
		Effects:               effects,
		Governance:            governance,
		FailurePolicyRef:      def.FailurePolicyRef,
		CancellationPolicyRef: def.CancellationPolicyRef,
		MigrationPolicyRef:    def.MigrationPolicyRef,
		RetentionPolicyRef:    def.RetentionPolicyRef,
		Concurrency:           concurrency,
		References:            refs.all,
	}

	ids := g.sortedNodeIDs()
	safePoints := placeSafePoints(def, g, records, concurrency)
	for _, id := range ids {
		n := g.nodes[id]
		cn := CompiledNode{
			ID:              n.ID,
			Type:            n.Type,
			Depth:           g.depth[id],
			SafePoint:       safePoints[id],
			InputSchema:     n.InputSchema,
			OutputSchema:    n.OutputSchema,
			Inputs:          cloneFields(n.Inputs),
			Outputs:         cloneFields(n.Outputs),
			RequiredContext: cloneContext(n.RequiredContext),
			Routes:          routeKeysOf(g, id),
			EffectClass:     effectClassOf(n, records),
			AllowedModes:    allowedModesFor(effectClassOf(n, records)),
			FailureRoute:    n.FailureRoute,
			Governance:      compileGovernance(n.Governance),
			EvidenceRefs:    evidenceRefsFor(n),
			ResolverRef:     refs.at(id, refFieldResolver),
			TimeoutPolicy:   refs.at(id, refFieldTimeoutPolicy),
			CompensationRef: refs.at(id, refFieldCompensation),
		}
		cn.Mappings = compileMappings(def, g, n)
		cn.EffectKey = effectKeyOf(n, records)
		cn.EffectRole = n.EffectRole
		if n.Retry != nil {
			retry := *n.Retry
			cn.Retry = &retry
		}
		if rec, ok := records[id]; ok && n.Capability != nil {
			cn.Capability = &CompiledCapability{
				ID:                    rec.Definition.ID,
				Version:               rec.Definition.Version,
				Digest:                rec.Digest,
				Status:                rec.Status,
				OwnerDomain:           rec.Definition.OwnerDomain,
				RequestSchema:         schemaFromCapability(rec.Definition.RequestSchema),
				ResponseSchema:        schemaFromCapability(rec.Definition.ResponseSchema),
				EffectClass:           rec.Definition.EffectClass,
				AuthZScopeRef:         rec.Definition.AuthZScopeRef,
				IdempotencyPolicyRef:  rec.Definition.IdempotencyPolicyRef,
				AgentEligible:         rec.Definition.AgentEligible,
				OperationMode:         n.Capability.OperationMode,
				AuthorityScopes:       append([]string(nil), n.Capability.AuthorityScopes...),
				IdempotencyKeyMapping: n.Capability.IdempotencyKeyMapping,
				EffectBinding:         n.Capability.EffectBinding,
				ExpectedVersions:      append([]string(nil), n.Capability.ExpectedVersions...),
			}
		}
		if n.Decision != nil {
			cn.Decision = &CompiledDecision{
				EvaluatorRef:       n.Decision.EvaluatorRef,
				EvaluatorVersion:   n.Decision.EvaluatorVersion,
				RuleRef:            n.Decision.RuleRef,
				InputDigestProfile: n.Decision.InputDigestProfile,
				InputDigest:        mappingDigest(cn.Mappings),
				Routes:             append([]DecisionRoute(nil), n.Decision.Routes...),
				DefaultRoute:       n.Decision.DefaultRoute,
				Rule:               refs.at(id, refFieldRule),
			}
		}
		if n.Transform != nil {
			cn.Transform = &CompiledTransform{
				TransformRef:         n.Transform.TransformRef,
				Version:              n.Transform.Version,
				NormalizationProfile: n.Transform.NormalizationProfile,
				Lookups:              append([]TransformLookup(nil), n.Transform.Lookups...),
				InputDigest:          mappingDigest(cn.Mappings),
				Lineage:              taintLineage(n.Transform),
				OutputTaint:          n.Transform.OutputTaint,
				SanitizerReceiptRef:  n.Transform.SanitizerReceiptRef,
				Limits:               n.Transform.Limits,
			}
		}
		if n.Observe != nil {
			cn.Observe = &CompiledObserve{
				EvidenceKind:         n.Observe.EvidenceKind,
				SourceAuthority:      n.Observe.SourceAuthority,
				ExpectedStateFields:  append([]string(nil), n.Observe.ExpectedStateFields...),
				RequiredWatermarks:   append([]string(nil), n.Observe.RequiredWatermarks...),
				MaxAgeSeconds:        n.Observe.MaxAgeSeconds,
				ComparisonProfile:    n.Observe.ComparisonProfile,
				RetryExhaustionRoute: n.Observe.RetryExhaustionRoute,
			}
		}
		if n.Wait != nil {
			cn.Wait = &CompiledWait{
				WakeKind:              n.Wait.WakeKind,
				WakeInstant:           n.Wait.WakeInstant,
				WakeLocalDate:         n.Wait.WakeLocalDate,
				WakeLocalTime:         n.Wait.WakeLocalTime,
				Disambiguation:        n.Wait.Disambiguation,
				ZoneID:                n.Wait.ZoneID,
				ZoneTzdbVersion:       n.Wait.ZoneTzdbVersion,
				CalendarRef:           n.Wait.CalendarRef,
				CalendarVersion:       n.Wait.CalendarVersion,
				ReferenceUpdatePolicy: n.Wait.ReferenceUpdatePolicy,
			}
		}
		if n.Signal != nil {
			cn.Signal = &CompiledSignal{
				EventType:                n.Signal.EventType,
				CorrelationKeyExpression: n.Signal.CorrelationKeyExpression,
				ExpectedSchemaRef:        n.Signal.ExpectedSchemaRef,
				AcceptedSources:          append([]string(nil), n.Signal.AcceptedSources...),
				Ordering:                 string(n.Signal.Ordering),
				CloseAfterSeconds:        n.Signal.CloseAfterSeconds,
			}
		}
		if t, ok := terminals[id]; ok {
			term := *t
			cn.Terminal = &term
			plan.Terminals = append(plan.Terminals, term)
		}
		plan.Nodes = append(plan.Nodes, cn)
	}

	plan.Edges = append([]Edge(nil), def.Edges...)
	sort.SliceStable(plan.Edges, func(i, j int) bool {
		if plan.Edges[i].From != plan.Edges[j].From {
			return plan.Edges[i].From < plan.Edges[j].From
		}
		if plan.Edges[i].RouteKey != plan.Edges[j].RouteKey {
			return plan.Edges[i].RouteKey < plan.Edges[j].RouteKey
		}
		// A PARALLEL's fan-out route is the one place two edges legitimately
		// share a source and a route key (WF-COMP-004). Ordering those by
		// target as well is what keeps the plan a function of the graph
		// rather than of the order the author listed the branches in; for
		// every other step type this tiebreak never fires, because a repeated
		// route key is CodeDuplicateRoute.
		return plan.Edges[i].To < plan.Edges[j].To
	})

	plan.Reachability = ReachabilityProof{
		StartNodeID: def.StartNodeID,
		Order:       append([]string(nil), g.order...),
		Depth:       map[string]uint32{},
		BackEdges:   append([]Edge(nil), g.backEdges...),
	}
	for _, id := range ids {
		plan.Reachability.Depth[id] = g.depth[id]
		if g.nodes[id].Type == StepEnd {
			plan.Reachability.Terminals = append(plan.Reachability.Terminals, id)
		}
	}
	sort.Slice(plan.Terminals, func(i, j int) bool { return plan.Terminals[i].NodeID < plan.Terminals[j].NodeID })

	plan.digest = computePlanDigest(plan)
	return plan
}

func schemaFromCapability(s capability.SchemaRef) SchemaRef {
	return SchemaRef{SchemaID: s.SchemaID, Version: s.Version, ProtobufFullName: s.ProtobufFullName}
}

func cloneLimits(l Limits) Limits {
	l.DeclaredCycles = append([]CycleDeclaration(nil), l.DeclaredCycles...)
	return l
}

func cloneContext(reqs []ContextRequirement) []ContextRequirement {
	if reqs == nil {
		return nil
	}
	out := make([]ContextRequirement, len(reqs))
	for i, r := range reqs {
		r.FieldPaths = append([]string(nil), r.FieldPaths...)
		r.RequiredWatermarks = append([]string(nil), r.RequiredWatermarks...)
		out[i] = r
	}
	return out
}

func compileGovernance(gv NodeGovernance) CompiledGovernance {
	return CompiledGovernance{
		Purpose:               gv.Purpose,
		Classification:        gv.Classification,
		RequiredDecisions:     append([]GovernanceKind(nil), gv.RequiredDecisions...),
		ObligationRefs:        append([]string(nil), gv.ObligationRefs...),
		ApprovalRequirements:  append([]string(nil), gv.ApprovalRequirements...),
		RevalidationBoundary:  gv.RevalidationBoundary,
		DataAccessManifestRef: gv.DataAccessManifestRef,
		OutputValidatorRef:    gv.OutputValidatorRef,
	}
}

func routeKeysOf(g *graph, id string) []string {
	edges := g.out[id]
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		out = append(out, e.RouteKey)
	}
	sort.Strings(out)
	return out
}

// evidenceRefsFor names the evidence a node execution records. A CAPABILITY
// node persists the invocation identity, not a copy of the whole result.
func evidenceRefsFor(n *Node) []string {
	switch n.Type {
	case StepCapability:
		return []string{"capability_execution_id", "governance_decision_id", "request_digest", "output_digest"}
	case StepObserve:
		return []string{"observation_id", "source_watermark", "reconciliation_result_id"}
	case StepDecision:
		return []string{"decision_id", "evaluated_input_digest", "evaluation_trace_ref"}
	case StepTransform:
		return []string{"transform_execution_id", "input_digest", "taint_manifest_ref"}
	case StepEnd:
		return []string{"workflow_result_id"}
	case StepWait:
		return []string{"timer_requirement_digest", "timer_resolution_digest"}
	case StepSignal:
		return []string{"subscription_digest", "signal_log_entry_digest"}
	default:
		return []string{"node_execution_id"}
	}
}

// placeSafePoints decides where safe points go. The compiler places them, not
// the author: a safe point sits before every node that can produce an
// irreversible or external effect, and on every terminal.
//
// An author's [Node.SafePointRequested] is honored only where WF-COMP-004's
// analysis proved the position is outside every atomic region -- and a
// request inside one has already been refused as [CodeUnsafeCheckpoint], so
// no plan reaches this function carrying an unsafe request. The request is
// therefore an input to a compiled decision, never the decision itself.
func placeSafePoints(
	def *Definition, g *graph, records map[string]capability.Record, conc *ConcurrencySummary,
) map[string]bool {
	ineligible := map[string]bool{}
	if conc != nil {
		for _, id := range conc.InterventionIneligibleNodes {
			ineligible[id] = true
		}
	}
	out := map[string]bool{}
	for i := range def.Nodes {
		n := &def.Nodes[i]
		class := effectClassOf(n, records)
		if class == capability.EffectExternalMutation || class == capability.EffectIrreversibleExternalMutation {
			for _, e := range g.in[n.ID] {
				out[e.From] = true
			}
			out[n.ID] = true
		}
		if n.Type == StepEnd {
			out[n.ID] = true
		}
		if n.SafePointRequested && !ineligible[n.ID] {
			out[n.ID] = true
		}
	}
	return out
}

func compileMappings(def *Definition, g *graph, n *Node) []CompiledMapping {
	inputs := fieldsByPath(n.Inputs)
	out := make([]CompiledMapping, 0, len(n.InputMappings))
	for _, m := range n.InputMappings {
		cm := CompiledMapping{
			Target:      m.Target,
			TargetType:  inputs[m.Target].clone(),
			SourceKind:  m.Source.Kind,
			SourceNode:  m.Source.NodeID,
			SourceCtx:   m.Source.ContextKind,
			SourcePath:  m.Source.Path,
			Constant:    m.Source.Constant,
			PinnedInput: isPinnedSource(n, m.Source),
		}
		if t, ok := sourceType(def, g, n, m.Source); ok {
			cm.SourceType = t.clone()
		}
		out = append(out, cm)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out
}
