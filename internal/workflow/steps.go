package workflow

import (
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
)

// StepReport is the conformance verdict for one node.
type StepReport struct {
	NodeID      string
	Type        StepType
	Conformance Conformance
	Errors      []Error
}

// OK reports whether the node conforms to its step-type contract.
func (r StepReport) OK() bool { return len(r.Errors) == 0 }

// CheckStepConformance validates one node against the fixed contract of its
// step type, in isolation from any graph. rec is the resolved capability
// record for nodes that bind one, or nil.
//
// This is the harness the step todos drive directly: it answers "is this a
// conforming CAPABILITY/DECISION/TRANSFORM/OBSERVE node?" without requiring a
// whole publishable definition around it. [Compile] runs exactly these checks
// plus the ones that need the graph.
func CheckStepConformance(n Node, rec *capability.Record) StepReport {
	c := &collector{}
	conf, ok := ConformanceFor(n.Type)
	if !ok {
		c.add(CodeInvalidDefinition, Location{NodeID: n.ID},
			"step type %q is not a kernel primitive", string(n.Type))
		return StepReport{NodeID: n.ID, Type: n.Type, Errors: c.errs}
	}
	stepDiagnostics(&n, rec, c)
	return StepReport{NodeID: n.ID, Type: n.Type, Conformance: conf, Errors: c.errs}
}

// checkSteps runs the step-type conformance pass over the whole definition,
// including the checks that need the compiled graph.
func checkSteps(def *Definition, g *graph, records map[string]capability.Record, c *collector) {
	for _, id := range g.sortedNodeIDs() {
		n := g.nodes[id]
		var rec *capability.Record
		if r, ok := records[id]; ok {
			rec = &r
		}
		stepDiagnostics(n, rec, c)
		if n.Type == StepObserve {
			checkObserveRoutes(g, n, c)
		}
	}
	_ = def
}

func stepDiagnostics(n *Node, rec *capability.Record, c *collector) {
	// Every capability-binding step type — CAPABILITY, OBSERVE and, in P1B,
	// COMPENSATE — proves the same binding contract: exact schemas, covered
	// authority scope and declared typed outputs.
	if conf, ok := ConformanceFor(n.Type); ok && conf.RequiresCapability {
		checkCapabilityBinding(n, rec, c)
	}
	switch n.Type {
	case StepDecision:
		checkDecisionStep(n, c)
	case StepTransform:
		checkTransformStep(n, c)
	case StepObserve:
		checkObserveStep(n, rec, c)
	case StepWait:
		checkWaitStep(n, c)
	case StepSignal:
		checkSignalStep(n, c)
	}
	if n.Retry != nil && n.Retry.BackoffRef == "" {
		c.add(CodeInvalidDefinition, Location{NodeID: n.ID, Field: "retry"},
			"a retry policy names the backoff policy it uses")
	}
}

// checkCapabilityBinding proves an exact, authorized, schema-compatible
// invocation (WF-STEP-001). It applies to every step type that binds a
// capability version, not only to CAPABILITY: an OBSERVE that names the wrong
// schema or an authority it was never granted is the same defect.
func checkCapabilityBinding(n *Node, rec *capability.Record, c *collector) {
	loc := Location{NodeID: n.ID}
	if n.Capability == nil {
		return
	}
	if len(n.Outputs) == 0 {
		c.add(CodeUnresolvedRef, loc,
			"a %s node declares the typed output fields it binds; it never exposes an unrestricted result", n.Type)
	}
	if !n.Capability.OperationMode.Valid() {
		c.add(CodeInvalidDefinition, Location{NodeID: n.ID, Field: "operation_mode"},
			"operation_mode %q is not a declared execution mode", string(n.Capability.OperationMode))
	}
	if rec == nil {
		return
	}
	ref := Location{NodeID: n.ID, Ref: rec.Definition.Key().String()}

	if !n.InputSchema.matchesCapabilitySchema(rec.Definition.RequestSchema) {
		c.add(CodeTypeMismatch, Location{NodeID: n.ID, Field: "input_schema", Ref: rec.Definition.Key().String()},
			"node binds input schema %s but the capability declares %s/v%d",
			n.InputSchema, rec.Definition.RequestSchema.SchemaID, rec.Definition.RequestSchema.Version)
	}
	if !n.OutputSchema.matchesCapabilitySchema(rec.Definition.ResponseSchema) {
		c.add(CodeTypeMismatch, Location{NodeID: n.ID, Field: "output_schema", Ref: rec.Definition.Key().String()},
			"node binds output schema %s but the capability declares %s/v%d",
			n.OutputSchema, rec.Definition.ResponseSchema.SchemaID, rec.Definition.ResponseSchema.Version)
	}

	scopes := map[string]bool{}
	for _, s := range n.Capability.AuthorityScopes {
		scopes[s] = true
	}
	if !scopes[rec.Definition.AuthZScopeRef] {
		c.add(CodeUnauthorizedScope, ref,
			"capability requires scope %q, which the node's declared authority does not carry",
			rec.Definition.AuthZScopeRef)
	}

	if n.Capability.OperationMode == ModeSimulate && rec.Definition.EffectClass.IsWrite() {
		c.add(CodeMutationInSimulation, ref,
			"invocation declares SIMULATE but the capability declares %s", rec.Definition.EffectClass)
	}
	if rec.Definition.EffectClass.IsWrite() && n.Capability.EffectBinding == "" {
		c.add(CodeNonIdempotentRetry, ref,
			"a mutating invocation binds a logical effect identity; none is declared")
	}
}

// checkDecisionStep proves a deterministic, pinned, mutually exclusive choice
// (WF-STEP-002).
func checkDecisionStep(n *Node, c *collector) {
	loc := Location{NodeID: n.ID}
	d := n.Decision
	if d == nil {
		return
	}
	if d.EvaluatorRef == "" || d.EvaluatorVersion == 0 {
		c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "evaluator_ref"},
			"a DECISION records the evaluator and version that produced the route")
	}
	if d.InputDigestProfile == "" {
		c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "input_digest_profile"},
			"a DECISION records the canonical profile its pinned input snapshot is digested under")
	}
	if len(d.Routes) == 0 {
		c.add(CodeMissingRoute, loc, "a DECISION declares at least one explicit route")
	}

	keys := map[string]bool{}
	byPredicate := map[string][]DecisionRoute{}
	for _, r := range d.Routes {
		rloc := Location{NodeID: n.ID, RouteKey: r.Key}
		if r.Key == "" {
			c.add(CodeInvalidDefinition, loc, "a declared route has no key")
			continue
		}
		if keys[r.Key] {
			c.add(CodeDuplicateRoute, rloc, "route key is declared more than once")
			continue
		}
		keys[r.Key] = true
		if r.Predicate == "" {
			c.add(CodeUnresolvedRef, rloc, "route declares no predicate")
			continue
		}
		byPredicate[r.Predicate] = append(byPredicate[r.Predicate], r)
	}
	predicates := make([]string, 0, len(byPredicate))
	for p := range byPredicate {
		predicates = append(predicates, p)
	}
	sort.Strings(predicates)
	for _, p := range predicates {
		rs := byPredicate[p]
		if len(rs) < 2 {
			continue
		}
		seen := map[int]bool{}
		for _, r := range rs {
			if seen[r.Precedence] {
				c.add(CodeNonExclusiveRoutes, Location{NodeID: n.ID, RouteKey: r.Key},
					"routes sharing predicate %q must declare distinct precedence", p)
				break
			}
			seen[r.Precedence] = true
		}
	}
	if d.DefaultRoute != "" && !keys[d.DefaultRoute] {
		c.add(CodeUnresolvedRef, Location{NodeID: n.ID, RouteKey: d.DefaultRoute},
			"default_route names no declared route; a default is explicit, never positional")
	}

	for _, m := range n.InputMappings {
		if !isPinnedSource(n, m.Source) {
			c.add(CodeMutableDecisionInput, Location{NodeID: n.ID, Field: m.Target},
				"a DECISION reads a pinned snapshot; context %q is not pinned", m.Source.ContextKind)
		}
	}
	for _, req := range n.RequiredContext {
		if !req.Pinned {
			c.add(CodeMutableDecisionInput, Location{NodeID: n.ID, Field: req.Kind},
				"a DECISION never performs a hidden current-state read; pin the context snapshot")
		}
	}
}

var taintRank = map[TaintLevel]int{TaintTrusted: 0, TaintDerived: 1, TaintTainted: 2}

// checkTransformStep proves a deterministic, bounded, side-effect-free mapping
// with satisfied taint lineage (WF-STEP-010).
func checkTransformStep(n *Node, c *collector) {
	loc := Location{NodeID: n.ID}
	t := n.Transform
	if t == nil {
		return
	}
	if t.TransformRef == "" || t.Version == 0 {
		c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "transform_ref"},
			"a TRANSFORM binds an exact versioned transform")
	}
	if t.NormalizationProfile == "" {
		c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "normalization_profile"},
			"a TRANSFORM declares its normalization profile")
	}
	if t.InlineCode != "" {
		c.add(CodeArbitraryCode, loc,
			"a TRANSFORM references a published transform; arbitrary customer code is out of scope")
	}
	for _, nd := range []struct {
		field string
		used  bool
	}{
		{"uses_clock", t.UsesClock},
		{"uses_random", t.UsesRandom},
		{"uses_network", t.UsesNetwork},
	} {
		if nd.used {
			c.add(CodeNondeterministicTransform, Location{NodeID: n.ID, Field: nd.field},
				"a TRANSFORM is deterministic and replayable; %s makes it neither", nd.field)
		}
	}
	for _, l := range t.Lookups {
		if l.Ref == "" || l.SnapshotDigest == "" {
			c.add(CodeUnpinnedLookup, Location{NodeID: n.ID, Ref: l.Ref},
				"a reference lookup is pinned to a snapshot digest")
		}
	}
	limits := []struct {
		field   string
		value   uint64
		ceiling uint64
	}{
		{"max_input_bytes", t.Limits.MaxInputBytes, MaxTransformInputBytes},
		{"max_output_bytes", t.Limits.MaxOutputBytes, MaxTransformOutputBytes},
		{"max_steps", t.Limits.MaxSteps, MaxTransformSteps},
	}
	for _, l := range limits {
		switch {
		case l.value == 0:
			c.add(CodeResourceLimitExceeded, Location{NodeID: n.ID, Field: l.field},
				"a TRANSFORM declares a bounded %s", l.field)
		case l.value > l.ceiling:
			c.add(CodeResourceLimitExceeded, Location{NodeID: n.ID, Field: l.field},
				"%s %d exceeds the compiler ceiling %d", l.field, l.value, l.ceiling)
		}
	}

	if _, ok := taintRank[t.OutputTaint]; !ok {
		c.add(CodeInvalidDefinition, Location{NodeID: n.ID, Field: "output_taint"},
			"output_taint %q is not a declared taint level", string(t.OutputTaint))
		return
	}
	inputs := fieldsByPath(n.Inputs)
	worst := TaintTrusted
	for _, in := range t.InputTaint {
		iloc := Location{NodeID: n.ID, Field: in.Source}
		if _, ok := inputs[in.Source]; !ok {
			c.add(CodeUnresolvedRef, iloc, "taint manifest names no declared input field")
			continue
		}
		if _, ok := taintRank[in.Level]; !ok {
			c.add(CodeInvalidDefinition, iloc, "taint level %q is not declared", string(in.Level))
			continue
		}
		if taintRank[in.Level] > taintRank[worst] {
			worst = in.Level
		}
	}
	if taintRank[worst] > taintRank[t.OutputTaint] && t.SanitizerReceiptRef == "" {
		c.add(CodeUnsatisfiedSanitizer, loc,
			"input taint %s is downgraded to %s with no sanitizer receipt", worst, t.OutputTaint)
	}
}

// checkObserveStep proves an authoritative, fresh, honestly-routed observation
// (WF-STEP-014).
func checkObserveStep(n *Node, rec *capability.Record, c *collector) {
	loc := Location{NodeID: n.ID}
	o := n.Observe
	if o == nil {
		return
	}
	if o.EvidenceKind != EvidenceAuthoritativeRead {
		c.add(CodeReceiptIsNotObservation, Location{NodeID: n.ID, Field: "evidence_kind"},
			"%s is not an observation of business state; only %s is",
			o.EvidenceKind, EvidenceAuthoritativeRead)
	}
	if o.SourceAuthority == "" {
		c.add(CodeReceiptIsNotObservation, Location{NodeID: n.ID, Field: "source_authority"},
			"an observation names the authority whose state it read")
	}
	if len(o.ExpectedStateFields) == 0 {
		c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "expected_state_fields"},
			"an observation reconciles against expected state; none is declared")
	}
	inputs := fieldsByPath(n.Inputs)
	for _, f := range o.ExpectedStateFields {
		if _, ok := inputs[f]; !ok {
			c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: f},
				"expected state field is not a declared input of this node")
		}
	}
	if len(o.RequiredWatermarks) == 0 {
		c.add(CodeStaleObservationAccepted, Location{NodeID: n.ID, Field: "required_watermarks"},
			"an observation declares the source watermarks that prove it is not stale")
	}
	if o.MaxAgeSeconds == 0 {
		c.add(CodeStaleObservationAccepted, Location{NodeID: n.ID, Field: "max_age_seconds"},
			"an observation declares a freshness bound")
	}
	if o.ComparisonProfile == "" {
		c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "comparison_profile"},
			"an observation declares the comparison profile it reconciles under")
	}
	if rec != nil && rec.Definition.EffectClass.IsWrite() {
		c.add(CodeEffectDeclarationConflict, loc,
			"an OBSERVE reads; capability %s declares %s", rec.Definition.Key(), rec.Definition.EffectClass)
	}
	if n.Retry != nil && n.Retry.MaxAttempts > 1 && o.RetryExhaustionRoute == "" {
		c.add(CodeRetryExhaustionFalseCompletion, Location{NodeID: n.ID, Field: "retry_exhaustion_route"},
			"bounded retry ends in an explicit degraded or repair route, never in a silent pass")
	}
}

// checkWaitStep proves a WAIT node declares a complete, unambiguous wake
// condition and the dataset identity a future timer must carry (WF-STEP-005):
// the wake condition names a fixed instant, a local date/time or a time expression, a
// local condition declares its DST disambiguation policy, and the zone,
// calendar and reference-update policy are always present so a later replay
// can defend the fire-at instant it produced.
func checkWaitStep(n *Node, c *collector) {
	w := n.Wait
	if w == nil {
		return
	}
	loc := Location{NodeID: n.ID}
	switch w.WakeKind {
	case WaitWakeAtInstant:
		if w.WakeInstant == "" {
			c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "wake_instant"},
				"a WAIT node declares AT_INSTANT but names no fixed instant")
		}
		if w.WakeLocalDate != "" || w.WakeLocalTime != "" {
			c.add(CodeInvalidDefinition, loc,
				"a WAIT node names both a fixed instant and a local wake condition; exactly one is legal")
		}
	case WaitWakeAtTimeExpr:
		if w.WakeInstant != "" || w.WakeLocalDate != "" || w.WakeLocalTime != "" {
			c.add(CodeInvalidDefinition, loc,
				"an AT_TIME_EXPR wake names a fixed instant or local wake condition alongside the expression; exactly one is legal")
		}
		if w.TimeExpr == nil {
			c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "time_expr"},
				"a WAIT node declares AT_TIME_EXPR but carries no time expression")
		} else {
			validTimeExprSpec(w.TimeExpr, true, n, "time_expr", c)
		}
	case WaitWakeAtLocalDate, WaitWakeAtLocalDateTime:
		if w.WakeLocalDate == "" {
			c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "wake_local_date"},
				"a local WAIT wake condition names no local date")
		}
		if w.WakeKind == WaitWakeAtLocalDateTime && w.WakeLocalTime == "" {
			c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "wake_local_time"},
				"AT_LOCAL_DATETIME names no local time")
		}
		if w.WakeKind == WaitWakeAtLocalDate && w.WakeLocalTime != "" {
			c.add(CodeInvalidDefinition, loc,
				"AT_LOCAL_DATE names a local time; declare AT_LOCAL_DATETIME instead")
		}
		if w.WakeInstant != "" {
			c.add(CodeInvalidDefinition, loc,
				"a WAIT node names both a fixed instant and a local wake condition; exactly one is legal")
		}
		if w.Disambiguation == "" {
			c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "disambiguation"},
				"a local wake condition declares a DST disambiguation policy")
		} else if !validDisambiguation(w.Disambiguation) {
			c.add(CodeInvalidDefinition, Location{NodeID: n.ID, Field: "disambiguation"},
				"disambiguation %q is not a declared policy", w.Disambiguation)
		}
	default:
		c.add(CodeInvalidDefinition, Location{NodeID: n.ID, Field: "wake_kind"},
			"wake_kind %q is not a declared WAIT wake condition", string(w.WakeKind))
		return
	}
	if w.ZoneID == "" || w.ZoneTzdbVersion == "" {
		c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "zone"},
			"a WAIT node declares the IANA zone and tzdb version its wake condition resolves against")
	}
	if w.CalendarRef == "" || w.CalendarVersion == "" {
		c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "calendar"},
			"a WAIT node declares the business calendar and dataset version it is pinned against")
	}
	if w.ReferenceUpdatePolicy == "" {
		c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "reference_update_policy"},
			"a WAIT node declares PIN, RECALCULATE or REVIEW_REQUIRED for a dataset republish")
	} else if !validReferenceUpdatePolicy(w.ReferenceUpdatePolicy) {
		c.add(CodeInvalidDefinition, Location{NodeID: n.ID, Field: "reference_update_policy"},
			"reference_update_policy %q is not declared", w.ReferenceUpdatePolicy)
	}
}

// validDisambiguation reports whether s names a declared DST disambiguation
// policy. It is a plain string set here — not internal/kernel/values.
// Disambiguation itself — so this package can validate a definition's shape
// without importing the runtime value package; the binding step
// (wait.FromCompiled) is what parses it into the typed value.
func validDisambiguation(s string) bool {
	switch s {
	case "REJECT_GAP", "EARLIER", "LATER", "EXPLICIT_OFFSET":
		return true
	default:
		return false
	}
}

// validReferenceUpdatePolicy reports whether s names a declared
// reference-update policy, mirroring
// internal/kernel/values.ReferenceUpdatePolicy's wire vocabulary.
func validReferenceUpdatePolicy(s string) bool {
	switch s {
	case "PIN", "RECALCULATE", "REVIEW_REQUIRED":
		return true
	default:
		return false
	}
}

// checkSignalStep proves a SIGNAL node declares the correlation, schema and
// source identity a durable subscription must carry (WF-STEP-006): an event
// type and correlation key expression, an expected schema, at least one
// accepted source, and a declared ordering expectation.
func checkSignalStep(n *Node, c *collector) {
	s := n.Signal
	if s == nil {
		return
	}
	if s.EventType == "" {
		c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "event_type"},
			"a SIGNAL node names no event type to correlate against")
	}
	if s.CorrelationKeyExpression == "" {
		c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "correlation_key_expression"},
			"a SIGNAL node names no correlation key expression")
	}
	if !s.ExpectedSchemaRef.Valid() {
		c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "expected_schema_ref"},
			"expected_schema_ref does not resolve to a versioned schema")
	}
	if len(s.AcceptedSources) == 0 {
		c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "accepted_sources"},
			"a SIGNAL node accepts no source; a subscription that accepts anyone is not a subscription")
	}
	if s.Ordering == "" {
		c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "ordering"},
			"a SIGNAL node declares no ordering expectation")
	} else if !s.Ordering.Valid() {
		c.add(CodeInvalidDefinition, Location{NodeID: n.ID, Field: "ordering"},
			"ordering %q is not a declared ordering expectation", string(s.Ordering))
	}
	if s.CloseTimeExpr != nil && s.CloseAfterSeconds != 0 {
		c.add(CodeInvalidDefinition, Location{NodeID: n.ID, Field: "close_time_expr"},
			"a SIGNAL node names both close_after_seconds and a close time expression; exactly one is legal")
	}
	if s.CloseTimeExpr != nil {
		validTimeExprSpec(s.CloseTimeExpr, false, n, "close_time_expr", c)
	}
	if s.TimeoutDefault != nil {
		if s.TimeoutDefault.Schema == "" || s.TimeoutDefault.CanonicalJSON == "" {
			c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "timeout_default"},
				"a SIGNAL timeout default names a schema and carries canonical payload bytes")
		}
	}
}

// validTimeExprSpec proves the shape of one TimeExprSpec without importing
// the runtime value package: the anchor mapping, the offset kind and the
// fields that kind requires. When inherit is true the expression must leave
// its zone and calendar halves empty because the owning spec pins them at
// node level; otherwise it must declare them itself. Typed parsing stays in
// the step packages FromCompiled translators.
func validTimeExprSpec(s *TimeExprSpec, inherit bool, n *Node, field string, c *collector) {
	loc := Location{NodeID: n.ID, Field: field}
	if s.AnchorPath == "" {
		c.add(CodeUnresolvedRef, loc, "a time expression maps no anchor into run data")
	}
	switch s.OffsetKind {
	case "BUSINESS_DAYS", "CALENDAR_DAYS":
		if s.Disambiguation == "" {
			c.add(CodeUnresolvedRef, loc, "a day-based time expression declares no DST disambiguation policy")
		} else if !validDisambiguation(s.Disambiguation) {
			c.add(CodeInvalidDefinition, loc,
				"disambiguation %q is not a declared policy", s.Disambiguation)
		}
	case "DURATION":
		if s.OffsetSeconds == 0 {
			c.add(CodeInvalidDefinition, loc, "a duration time expression offsets by zero seconds")
		}
	default:
		c.add(CodeInvalidDefinition, loc,
			"offset kind %q is not a declared time-expression offset", s.OffsetKind)
	}
	if inherit {
		if s.ZoneID != "" || s.ZoneTzdbVersion != "" || s.CalendarRef != "" || s.CalendarVersion != "" {
			c.add(CodeInvalidDefinition, loc,
				"a WAIT time expression pins its own zone or calendar; the node-level halves govern")
		}
	} else {
		if s.ZoneID == "" || s.ZoneTzdbVersion == "" {
			c.add(CodeUnresolvedRef, loc, "a time expression declares no zone and tzdb version")
		}
		if s.CalendarRef == "" || s.CalendarVersion == "" {
			c.add(CodeUnresolvedRef, loc, "a time expression declares no business calendar and dataset version")
		}
	}
	if (s.StatuteRulePack == "") != (s.Statute == "") {
		c.add(CodeInvalidDefinition, loc, "a statute reference names both a rule pack and a statute")
	}
}

// checkObserveRoutes proves a degraded observation does not continue as if it
// had passed, and that retry exhaustion reaches a degraded or repair terminal.
func checkObserveRoutes(g *graph, n *Node, c *collector) {
	targets := map[string]string{}
	for _, e := range g.out[n.ID] {
		targets[e.RouteKey] = e.To
	}
	pass, hasPass := targets[string(OutcomePass)]
	if hasPass {
		for _, degraded := range []Outcome{OutcomeUnknown, OutcomePartial, OutcomeFail} {
			if to, ok := targets[string(degraded)]; ok && to == pass {
				c.add(CodeDegradedCollapsedToPass,
					Location{NodeID: n.ID, RouteKey: string(degraded), EdgeFrom: n.ID, EdgeTo: to},
					"outcome %s continues exactly as PASS does; a stale, partial or unavailable result is not a pass",
					degraded)
			}
		}
	}

	if n.Observe == nil || n.Observe.RetryExhaustionRoute == "" {
		return
	}
	route := n.Observe.RetryExhaustionRoute
	if _, ok := g.nodes[route]; !ok {
		return
	}
	reached := false
	for _, id := range g.sortedNodeIDs() {
		end := g.nodes[id]
		if end.Type != StepEnd || !g.reaches(route, id) {
			continue
		}
		reached = true
		if endClaimsCleanSuccess(end) {
			c.add(CodeRetryExhaustionFalseCompletion, Location{NodeID: n.ID, Ref: id},
				"retry exhaustion reaches terminal %q, which claims a completed, consistent outcome", id)
		}
	}
	if !reached {
		c.add(CodeRetryExhaustionFalseCompletion, Location{NodeID: n.ID, Ref: route},
			"retry exhaustion route %q reaches no terminal", route)
	}
}

// endClaimsCleanSuccess reports whether a terminal claims both a completed
// business outcome and external consistency.
func endClaimsCleanSuccess(n *Node) bool {
	if n.End == nil {
		return false
	}
	m := n.End.CompletionMapping
	return m[string(lifecycle.DimensionBusiness)] == string(lifecycle.BusinessCompleted.StateID()) &&
		m[string(lifecycle.DimensionConsistency)] == string(lifecycle.ConsistencyConsistent.StateID())
}
