package execute

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// WF-EXT-004: the durable data plane a mapping resolves against on the
// EXECUTE path. Everything in this file is plumbing around two primitives
// internal/workflow/runtime already owns (RecordWorkflowInputs/
// LoadWorkflowInputs, RecordNodeOutputs/LoadNodeOutputs) and one resolver
// internal/workflow already owns (workflow.ResolveMappings) -- the driver
// never re-implements resolution, it only wires durable rows to it.

// artifactValuesOf converts the typed values a step runner or a start
// request carries into the shape [runtime.WorkflowInputArtifact] and
// [runtime.NodeOutputArtifact] store.
func artifactValuesOf(outs []workflow.TypedOutput) []runtime.TypedArtifactValue {
	vals := make([]runtime.TypedArtifactValue, 0, len(outs))
	for _, o := range outs {
		vals = append(vals, runtime.TypedArtifactValue{Path: o.Path, Type: o.Value.Type, Value: o.Value.Text})
	}
	return vals
}

// validateWorkflowInputDocument refuses a workflow-input document that does
// not exactly match the compiled plan's declared workflow inputs: every
// declared input present, at an assignable type, and nothing undeclared.
func validateWorkflowInputDocument(plan *workflow.CompiledWorkflow, inputs []workflow.TypedOutput) error {
	if plan == nil {
		return invalid("no compiled plan to validate the workflow input document against")
	}
	declared := make(map[string]workflow.ValueType, len(plan.Inputs))
	for _, f := range plan.Inputs {
		declared[f.Path] = f.Type
	}
	seen := make(map[string]bool, len(inputs))
	for _, in := range inputs {
		want, ok := declared[in.Path]
		if !ok {
			return invalid("workflow input document names %q, which %s does not declare as a workflow input", in.Path, plan.WorkflowID)
		}
		if seen[in.Path] {
			return invalid("workflow input document names %q more than once", in.Path)
		}
		seen[in.Path] = true
		if err := in.Value.Type.AssignableTo(want); err != nil {
			return invalid("workflow input %q: %s is not assignable to declared type %s: %v", in.Path, in.Value.Type, want, err)
		}
	}
	for _, f := range plan.Inputs {
		if !seen[f.Path] {
			return invalid("workflow input document is missing declared input %q", f.Path)
		}
	}
	return nil
}

// validateContextDocument binds each supplied context snapshot to the proof
// reference checked by runtime.Start and verifies mapped values against the
// compiled source and target types.
func validateContextDocument(plan *workflow.CompiledWorkflow, contexts []runtime.TypedContextSnapshot, references map[string]string) error {
	if plan == nil {
		return invalid("no compiled plan to validate the context document against")
	}
	targetTypes := map[string]map[string][]workflow.ValueType{}
	for _, node := range plan.Nodes {
		for _, mapping := range node.Mappings {
			if mapping.SourceKind != workflow.SourceContext {
				continue
			}
			byPath := targetTypes[mapping.SourceCtx]
			if byPath == nil {
				byPath = map[string][]workflow.ValueType{}
				targetTypes[mapping.SourceCtx] = byPath
			}
			byPath[mapping.SourcePath] = append(byPath[mapping.SourcePath], mapping.TargetType)
		}
	}
	seen := map[string]bool{}
	for _, snapshot := range contexts {
		if snapshot.Reference == "" || references[snapshot.Kind] != snapshot.Reference {
			return invalid("context %q snapshot reference does not match the start proof reference", snapshot.Kind)
		}
		if seen[snapshot.Kind] {
			return invalid("context document contains duplicate kind %q", snapshot.Kind)
		}
		seen[snapshot.Kind] = true
		declared := targetTypes[snapshot.Kind]
		for _, value := range snapshot.Values {
			wants, ok := declared[value.Path]
			if !ok {
				return invalid("context %q contains undeclared mapped field %q", snapshot.Kind, value.Path)
			}
			for _, want := range wants {
				if err := value.Type.AssignableTo(want); err != nil {
					return invalid("context %s.%s: %s is not assignable to mapped type %s: %v", snapshot.Kind, value.Path, value.Type, want, err)
				}
			}
		}
	}
	for kind, paths := range targetTypes {
		if references[kind] == "" {
			return invalid("context mapping for %q has no resolved proof reference", kind)
		}
		var snapshot *runtime.TypedContextSnapshot
		for i := range contexts {
			if contexts[i].Kind == kind {
				snapshot = &contexts[i]
				break
			}
		}
		if snapshot == nil {
			return invalid("context mapping for %q has no typed snapshot", kind)
		}
		values := map[string]bool{}
		for _, value := range snapshot.Values {
			values[value.Path] = true
		}
		for path := range paths {
			if !values[path] {
				return invalid("context snapshot %q is missing mapped field %q", kind, path)
			}
		}
	}
	return nil
}

// planForInputs resolves the compiled plan a start is binding, reusing an
// already-resolved selection when one is available (the serializable retry
// path's selection pointer, populated by the time runtime.Start returns) and
// falling back to a resolver call otherwise. On the non-serializable path
// req.Resolver is already a [fixedResolver] over the plan Execute resolved
// before calling startOnce, so that fallback call does no work of its own.
func (d *Driver) planForInputs(ctx context.Context, req runtime.StartRequest, selection *runtime.WorkflowSelection) (*workflow.CompiledWorkflow, error) {
	if selection != nil && selection.Plan != nil {
		return selection.Plan, nil
	}
	sel, err := req.Resolver.ResolveWorkflow(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("workflow execute: resolve workflow for start inputs: %w", err)
	}
	if sel.Plan == nil {
		return nil, invalid("workflow resolver returned no compiled plan for start inputs")
	}
	return sel.Plan, nil
}

// dataPlaneSource adapts the durable workflow-input artifact and the latest
// succeeded node-output artifacts of one instance to [workflow.MappingSource],
// so the durable driver resolves a compiled node's mappings through the exact
// resolver internal/workflow/simulate uses.
type dataPlaneSource struct {
	hasInputs bool
	inputs    runtime.WorkflowInputArtifact
	contexts  map[string][]runtime.TypedArtifactValue
	// producedOutputs holds, for a node whose latest recorded execution
	// SUCCEEDED, that attempt's typed outputs. A node absent from this map
	// either never ran, or its latest attempt did not succeed -- both read as
	// "has not run" to a mapping, which is the conservative reading: no
	// mapping may read an output a run cannot yet stand behind.
	producedOutputs map[string][]runtime.TypedArtifactValue
}

func (s dataPlaneSource) WorkflowInput(path string) (workflow.TypedValue, bool) {
	if !s.hasInputs {
		return workflow.TypedValue{}, false
	}
	v, ok := s.inputs.Input(path)
	if !ok {
		return workflow.TypedValue{}, false
	}
	return workflow.TypedValue{Type: v.Type, Text: v.Value}, true
}

func (s dataPlaneSource) NodeOutput(nodeID, path string) (workflow.TypedValue, bool, bool) {
	outputs, produced := s.producedOutputs[nodeID]
	if !produced {
		return workflow.TypedValue{}, false, false
	}
	for _, o := range outputs {
		if o.Path == path {
			return workflow.TypedValue{Type: o.Type, Text: o.Value}, true, true
		}
	}
	return workflow.TypedValue{}, true, false
}

// Context reads the typed snapshot bound to the resolved proof reference in
// the instance's immutable workflow input artifact.
func (s dataPlaneSource) Context(kind, path string) (workflow.TypedValue, bool) {
	for _, value := range s.contexts[kind] {
		if value.Path == path {
			return workflow.TypedValue{Type: value.Type, Text: value.Value}, true
		}
	}
	return workflow.TypedValue{}, false
}

// buildDataPlaneSource reads the durable rows one [workflow.ResolveMappings]
// call needs, in one short read-only-by-convention transaction. hasInputs is
// false, and every other field zero, for an instance that recorded neither a
// workflow-input nor context document -- the caller skips resolution when
// there is no durable document to resolve against.
func (d *Driver) buildDataPlaneSource(ctx context.Context, run runContext) (dataPlaneSource, error) {
	tx, err := d.opts.DB.Begin(ctx)
	if err != nil {
		return dataPlaneSource{}, fmt.Errorf("workflow execute: begin data plane read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, run.start.TenantID); err != nil {
		return dataPlaneSource{}, err
	}

	inputs, found, err := runtime.LoadWorkflowInputs(ctx, tx, run.start.TenantID, run.instanceID)
	if err != nil {
		return dataPlaneSource{}, err
	}
	if !found {
		return dataPlaneSource{}, nil
	}

	executions, err := (runtime.Store{}).LoadNodeExecutions(ctx, tx, run.start.TenantID, run.instanceID)
	if err != nil {
		return dataPlaneSource{}, err
	}
	latest := make(map[string]runtime.NodeExecution, len(executions))
	for _, ne := range executions {
		if cur, ok := latest[ne.NodeID]; !ok || ne.Attempt > cur.Attempt {
			latest[ne.NodeID] = ne
		}
	}

	outputs, err := runtime.LoadNodeOutputs(ctx, tx, run.start.TenantID, run.instanceID)
	if err != nil {
		return dataPlaneSource{}, err
	}
	produced := make(map[string][]runtime.TypedArtifactValue, len(outputs))
	for _, o := range outputs {
		ne, ok := latest[o.NodeID]
		if !ok || ne.Attempt != o.Attempt || ne.Status != runtime.NodeSucceeded {
			continue
		}
		produced[o.NodeID] = o.Outputs
	}

	if err := tx.Commit(ctx); err != nil {
		return dataPlaneSource{}, fmt.Errorf("workflow execute: commit data plane read: %w", err)
	}
	contexts := make(map[string][]runtime.TypedArtifactValue, len(inputs.Contexts))
	for _, snapshot := range inputs.Contexts {
		contexts[snapshot.Kind] = snapshot.Values
	}
	return dataPlaneSource{hasInputs: true, inputs: inputs, contexts: contexts, producedOutputs: produced}, nil
}

// resolveNodeInputs resolves node's compiled mappings against the instance's
// durable data plane, or reports nil when the instance recorded no
// workflow-input document at all (so there is nothing durable to resolve
// against and every StepRunner keeps building its own inputs, exactly as
// before WF-EXT-004).
func (d *Driver) resolveNodeInputs(ctx context.Context, run runContext, node workflow.CompiledNode) (map[string]workflow.TypedValue, error) {
	src, err := d.buildDataPlaneSource(ctx, run)
	if err != nil {
		return nil, fmt.Errorf("workflow execute: load data plane for node %s: %w", node.ID, err)
	}
	if !src.hasInputs {
		return nil, nil
	}
	resolved, err := workflow.ResolveMappings(node, src)
	if err != nil {
		return nil, fmt.Errorf("workflow execute: resolve mappings for node %s: %w", node.ID, err)
	}
	return resolved, nil
}

// recordOutcomeOutputs persists outcome.Outputs as a durable
// [runtime.NodeOutputArtifact], inside the advancement transaction ex, when
// the step runner produced any. It is a no-op for the outcome of every
// StepRunner that does not set Outputs, which is every StepRunner before
// WF-EXT-004.
//
// A runner that also set OutputDigest itself must agree with the artifact's
// own digest; disagreement is refused rather than silently preferring one
// over the other, because the two would then be describing different typed
// content under one recorded digest.
func (d *Driver) recordOutcomeOutputs(
	ctx context.Context, ex runtime.Executor, req StepRequest, attempt int, outcome frontier.NodeOutcome,
) (frontier.NodeOutcome, error) {
	if outcome.Outputs == nil || len(outcome.Outputs.Values) == 0 {
		return outcome, nil
	}
	artifact := runtime.NodeOutputArtifact{
		TenantID: req.TenantID, InstanceID: req.InstanceID, NodeID: req.Node.ID, Attempt: attempt,
		StepType: req.Node.Type, PlanDigest: req.Plan.Digest(),
		Outputs: artifactValuesOf(outcome.Outputs.Values), RecordedAt: req.RecordedAt,
	}
	digest, err := runtime.RecordNodeOutputs(ctx, ex, artifact, &req.Node)
	if err != nil {
		return frontier.NodeOutcome{}, fmt.Errorf("workflow execute: record node %s outputs: %w", req.Node.ID, err)
	}
	switch {
	case outcome.OutputDigest == "":
		outcome.OutputDigest = digest
	case outcome.OutputDigest != digest:
		return frontier.NodeOutcome{}, fmt.Errorf(
			"workflow execute: node %s reported output digest %s, but its recorded typed outputs digest to %s",
			req.Node.ID, outcome.OutputDigest, digest)
	}
	return outcome, nil
}
