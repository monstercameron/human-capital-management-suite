package workflow

import (
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// validateShape proves the definition is structurally well formed before any
// pass that assumes it is: identity present, node ids unique, step types
// declared and implemented in this phase, schemas resolvable, field paths
// unique, and exactly the step specification the node type requires.
func validateShape(def *Definition, opts Options, c *collector) {
	root := Location{Ref: def.WorkflowID}

	if def.WorkflowID == "" {
		c.add(CodeInvalidDefinition, root, "workflow_id is required")
	}
	if def.Version == 0 {
		c.add(CodeInvalidDefinition, root, "version is required and starts at 1")
	}
	if def.Name == "" {
		c.add(CodeInvalidDefinition, root, "name is required")
	}
	if def.TenantScope == "" {
		c.add(CodeInvalidDefinition, root, "tenant_scope is required")
	}
	if def.OrganizationScope == "" {
		c.add(CodeInvalidDefinition, root, "organization_scope is required")
	}
	if def.RiskClass == "" {
		c.add(CodeInvalidDefinition, root, "risk_class is required")
	}
	if def.StartNodeID == "" {
		c.add(CodeInvalidDefinition, root, "start_node_id is required")
	}
	if len(def.Nodes) == 0 {
		c.add(CodeInvalidDefinition, root, "a definition declares at least one node")
	}
	if !def.TerminalProfile.Valid() {
		c.add(CodeInvalidDefinition, root,
			"terminal_profile %q is not a declared profile", string(def.TerminalProfile))
	}
	if len(def.DeclaredModes) == 0 {
		c.add(CodeInvalidDefinition, root, "declared_modes is required")
	}
	for _, m := range def.DeclaredModes {
		if !m.Valid() {
			c.add(CodeInvalidDefinition, root, "declared mode %q is not a declared execution mode", string(m))
		}
	}
	for _, ref := range []struct {
		name string
		ref  SchemaRef
	}{
		{"input_schema", def.InputSchema},
		{"output_schema", def.OutputSchema},
		{"variables_schema", def.VariablesSchema},
	} {
		if !ref.ref.Valid() {
			c.add(CodeUnresolvedRef, Location{Ref: def.WorkflowID, Field: ref.name},
				"%s does not resolve to a versioned schema", ref.name)
		}
	}
	validateFields(def.Inputs, Location{Ref: def.WorkflowID, Field: "inputs"}, c)
	validateFields(def.Outputs, Location{Ref: def.WorkflowID, Field: "outputs"}, c)
	if len(def.Outputs) == 0 {
		c.add(CodeInvalidDefinition, root, "outputs is required: a workflow declares the result it produces")
	}

	seen := map[string]bool{}
	for i := range def.Nodes {
		n := &def.Nodes[i]
		loc := Location{NodeID: n.ID}
		if n.ID == "" {
			c.add(CodeInvalidDefinition, root, "node %d declares no id", i)
			continue
		}
		if seen[n.ID] {
			c.add(CodeInvalidDefinition, loc, "node id is declared more than once")
			continue
		}
		seen[n.ID] = true

		if !n.Type.Valid() {
			c.add(CodeInvalidDefinition, loc, "step type %q is not a kernel primitive", string(n.Type))
			continue
		}
		if !opts.phase().admits(n.Type) {
			conf, _ := ConformanceFor(n.Type)
			c.add(CodePhaseNotImplemented, loc,
				"%s is implemented in %s; this compiler runs in %s", n.Type, conf.Phase, opts.phase())
			continue
		}

		validateNodeShape(n, c)
	}

	for _, cycle := range def.Limits.DeclaredCycles {
		if cycle.EntryNodeID == "" || cycle.GuardNodeID == "" {
			c.add(CodeInvalidDefinition, root,
				"declared cycle requires both an entry node and a guard node")
		}
	}

	validateApprovalRequirements(def, c)
	validateObligations(def, c)
}

func validateFields(fields []Field, loc Location, c *collector) {
	seen := map[string]bool{}
	for _, f := range fields {
		fieldLoc := loc
		fieldLoc.Field = f.Path
		if f.Path == "" {
			c.add(CodeInvalidDefinition, loc, "a declared field has no path")
			continue
		}
		if seen[f.Path] {
			c.add(CodeInvalidDefinition, fieldLoc, "field path is declared more than once")
			continue
		}
		seen[f.Path] = true
		if err := f.Type.Validate(); err != nil {
			c.add(CodeInvalidDefinition, fieldLoc, "%v", err)
		}
	}
}

// validateNodeShape checks that a node carries exactly the specification its
// step type requires and nothing belonging to another type.
func validateNodeShape(n *Node, c *collector) {
	loc := Location{NodeID: n.ID}
	conf, _ := ConformanceFor(n.Type)

	validateFields(n.Inputs, loc, c)
	validateFields(n.Outputs, loc, c)

	if !conf.Terminal {
		if !n.InputSchema.Valid() {
			c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "input_schema"},
				"input_schema does not resolve to a versioned schema")
		}
		if !n.OutputSchema.Valid() {
			c.add(CodeUnresolvedRef, Location{NodeID: n.ID, Field: "output_schema"},
				"output_schema does not resolve to a versioned schema")
		}
	}

	if conf.RequiresCapability && n.Capability == nil {
		c.add(CodeUnresolvedRef, loc, "%s binds a capability version; none is declared", n.Type)
	}
	if !conf.RequiresCapability && n.Capability != nil {
		c.add(CodeInvalidDefinition, loc, "%s does not invoke a capability", n.Type)
	}

	type spec struct {
		name    string
		present bool
		wanted  bool
	}
	specs := []spec{
		{"decision", n.Decision != nil, n.Type == StepDecision},
		{"transform", n.Transform != nil, n.Type == StepTransform},
		{"observe", n.Observe != nil, n.Type == StepObserve},
		{"end", n.End != nil, n.Type == StepEnd},
		{"wait", n.Wait != nil, n.Type == StepWait},
		{"signal", n.Signal != nil, n.Type == StepSignal},
	}
	for _, s := range specs {
		switch {
		case s.wanted && !s.present:
			c.add(CodeInvalidDefinition, Location{NodeID: n.ID, Field: s.name},
				"%s requires a %s specification", n.Type, s.name)
		case !s.wanted && s.present:
			c.add(CodeInvalidDefinition, Location{NodeID: n.ID, Field: s.name},
				"%s must not declare a %s specification", n.Type, s.name)
		}
	}

	if n.DeclaredEffect != "" && !n.DeclaredEffect.Valid() {
		c.add(CodeInvalidDefinition, Location{NodeID: n.ID, Field: "declared_effect"},
			"%q is not one of the five declared effect classes", string(n.DeclaredEffect))
	}

	for _, req := range n.RequiredContext {
		reqLoc := Location{NodeID: n.ID, Field: req.Kind}
		if req.Kind == "" {
			c.add(CodeInvalidDefinition, loc, "a context requirement declares no kind")
			continue
		}
		if len(req.FieldPaths) == 0 {
			c.add(CodeInvalidDefinition, reqLoc,
				"a context requirement names the exact field paths it reads; the node never receives the whole artifact")
		}
		if req.Purpose == "" {
			c.add(CodeInvalidDefinition, reqLoc, "a context requirement declares its purpose")
		}
		if req.MaxAgeSeconds == 0 {
			c.add(CodeStaleObservationAccepted, reqLoc,
				"a context requirement declares a maximum age; unbounded context is silently stale context")
		}
		switch req.MissingBehavior {
		case MissingFail, MissingUnknown:
		default:
			c.add(CodeInvalidDefinition, reqLoc,
				"missing_behavior is %s or %s; a masked, stale or unknown value is never absent, zero or false",
				MissingFail, MissingUnknown)
		}
	}

	if n.Retry != nil && n.Retry.MaxAttempts == 0 {
		c.add(CodeInvalidDefinition, Location{NodeID: n.ID, Field: "retry"},
			"a retry policy declares a bounded max_attempts")
	}

	checkOutcomeAliases(n, conf, c)
	checkModeOverlay(n, c)
}

func validateApprovalRequirements(def *Definition, c *collector) {
	seen := map[string]bool{}
	for _, r := range def.ApprovalRequirements {
		loc := Location{Ref: r.ID}
		if r.ID == "" {
			c.add(CodeInvalidDefinition, Location{Ref: def.WorkflowID},
				"an approval requirement declares no id")
			continue
		}
		if seen[r.ID] {
			c.add(CodeInvalidDefinition, loc, "approval requirement id is declared more than once")
			continue
		}
		seen[r.ID] = true
		if r.ResolverExpression == "" {
			c.add(CodeUnresolvedApprovalScope, loc, "approval requirement declares no resolver expression")
		}
		if r.Scope == "" {
			c.add(CodeUnresolvedApprovalScope, loc, "approval requirement declares no scope")
		}
		if r.Quorum <= 0 {
			c.add(CodeUnresolvedApprovalScope, loc, "approval requirement declares no quorum")
		}
		if r.EffectiveAsOfPolicy == "" {
			c.add(CodeUnresolvedApprovalScope, loc, "approval requirement declares no effective-as-of policy")
		}
	}
}

func validateObligations(def *Definition, c *collector) {
	seen := map[string]bool{}
	for _, o := range def.Obligations {
		loc := Location{Ref: o.ID}
		if o.ID == "" {
			c.add(CodeUnresolvedObligation, Location{Ref: def.WorkflowID}, "an obligation declares no id")
			continue
		}
		if seen[o.ID] {
			c.add(CodeUnresolvedObligation, loc, "obligation id is declared more than once")
			continue
		}
		seen[o.ID] = true
		if o.Authority == "" {
			c.add(CodeUnresolvedObligation, loc, "obligation declares no issuing authority")
		}
		if !o.InsertionPoint.Valid() {
			c.add(CodeUnresolvedObligation, loc,
				"insertion_point %q is not a declared insertion point", string(o.InsertionPoint))
		}
		if o.RequiredAction == "" {
			c.add(CodeUnresolvedObligation, loc, "obligation declares no required action")
		}
		if o.ResponsibleParty == "" {
			c.add(CodeUnresolvedObligation, loc, "obligation declares no responsible party")
		}
		if o.SatisfactionCondition == "" {
			c.add(CodeUnresolvedObligation, loc, "obligation declares no satisfaction condition")
		}
		if o.SourceVersion == "" {
			c.add(CodeUnresolvedObligation, loc, "obligation declares no source rule version")
		}
		if !o.ReevaluationPolicy.Valid() {
			c.add(CodeUnresolvedObligation, loc,
				"reevaluation_policy %q is not declared; a rule change never silently rewrites a live instance",
				string(o.ReevaluationPolicy))
		}
	}
}

// resolveCapabilities resolves every bound capability version against the
// registry and refuses a missing, unknown or retired one. A retired version
// still resolves for history; it is never a publication target.
func resolveCapabilities(def *Definition, opts Options, c *collector) map[string]capability.Record {
	out := map[string]capability.Record{}
	for i := range def.Nodes {
		n := &def.Nodes[i]
		if n.Capability == nil {
			continue
		}
		loc := Location{NodeID: n.ID, Ref: n.Capability.Key().String()}
		if n.Capability.ID == "" || n.Capability.Version == 0 {
			c.add(CodeUnresolvedRef, loc, "capability reference names no exact (id, version)")
			continue
		}
		if opts.Capabilities == nil {
			c.add(CodeUnresolvedRef, loc, "no capability registry was supplied to the compiler")
			continue
		}
		rec, ok := opts.Capabilities.Lookup(n.Capability.Key())
		if !ok {
			c.add(CodeUnresolvedRef, loc, "capability version is not published")
			continue
		}
		if rec.Status == capability.StatusRetired {
			c.add(CodeUnresolvedRef, loc, "capability version is retired and cannot be bound by a new plan")
			continue
		}
		out[n.ID] = rec
	}
	return out
}

// sortedStrings returns a sorted, deduplicated copy.
func sortedStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
