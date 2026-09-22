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

// Context reports every CONTEXT mapping unresolved. The only per-instance
// context fact this package pins durably today is
// [runtime.StartRequest.ResolvedContext], a proof reference keyed by context
// kind (that a required-context read happened), not the typed per-path field
// values a mapping needs -- there is no durable store of those on the
// EXECUTE path yet. A CONTEXT mapping therefore refuses
// [workflow.CodeUnresolvedContext] rather than fabricate a value; see this
// ticket's final report for the follow-up this leaves open.
func (dataPlaneSource) Context(string, string) (workflow.TypedValue, bool) {
	return workflow.TypedValue{}, false
}

// buildDataPlaneSource reads the durable rows one [workflow.ResolveMappings]
// call needs, in one short read-only-by-convention transaction. hasInputs is
// false, and every other field zero, for an instance that recorded no
// workflow-input document -- the caller skips resolution entirely in that
// case, exactly reproducing the pre-WF-EXT-004 behavior for every plan that
// starts with no ExecuteRequest.Inputs.
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
	return dataPlaneSource{hasInputs: true, inputs: inputs, producedOutputs: produced}, nil
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
