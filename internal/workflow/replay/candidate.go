package replay

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// CandidateRequest is everything a [Candidate] may consult to recompute one
// pure node attempt: the compiled node from the pinned plan, and the inputs
// and versions the historical attempt was evaluated against, read from the
// durable record. There is no executor, adapter or clock in it on purpose: a
// candidate that needs current data has nowhere to get it from.
type CandidateRequest struct {
	Node    workflow.CompiledNode
	Attempt int
	// PlanDigest and ExecutionContextDigest are the record's own pins.
	PlanDigest             string
	ExecutionContextDigest string
	// Inputs is a copy of the pinned input artifact; a candidate mutating it
	// changes nothing.
	Inputs runtime.NodeInputArtifact
}

// CandidateOutcome is what a candidate implementation produced for one
// attempt. It is compared with the recorded route key, failure and output
// digest; the first difference is a [Divergence] naming the node.
type CandidateOutcome struct {
	RouteKey     string
	OutputDigest string
	Failed       bool
	ErrorClass   string
}

// Candidate is the implementation a replay recomputes a pure node with -- the
// current code for that node, run against the node's pinned historical
// inputs. Returning an [Error] with [CodeArtifactUnavailable] reports a
// pinned input or version the candidate needs and the record does not hold.
type Candidate interface {
	Recompute(ctx context.Context, req CandidateRequest) (CandidateOutcome, error)
}

// Candidates binds candidate implementations to nodes. ByNode wins over
// ByStepType. A [Replayer] always starts from [DefaultCandidates] and overlays
// the caller's bindings, so a DECISION node is recomputed unless the caller
// replaces the DECISION candidate with its own.
type Candidates struct {
	ByNode     map[string]Candidate
	ByStepType map[workflow.StepType]Candidate
}

// DefaultCandidates is the candidate set every replay starts from: DECISION
// nodes are recomputed by evaluating their pinned rule table.
func DefaultCandidates() Candidates {
	return Candidates{ByStepType: map[workflow.StepType]Candidate{
		workflow.StepDecision: RulesDecisionCandidate{Bindings: []RuleTableBinding{PromotionThresholdBinding()}},
	}}
}

// Recomputable reports whether a node may be recomputed at all: its compiled
// effect class is pure or read-only and its step type is one whose outcome is
// a deterministic function of its inputs (DECISION, TRANSFORM, or a read-only
// CAPABILITY whose inputs are pinned). Approvals, tasks, waits, signals,
// observations of live state, compensation, sub-workflows and terminals always
// take their recorded outcome.
func Recomputable(node workflow.CompiledNode) bool {
	if node.EffectClass != capability.EffectPure && node.EffectClass != capability.EffectReadOnly {
		return false
	}
	switch node.Type {
	case workflow.StepDecision, workflow.StepTransform, workflow.StepCapability:
		return true
	default:
		return false
	}
}

// merged overlays c on the defaults.
func (c Candidates) merged() Candidates {
	out := DefaultCandidates()
	out.ByNode = map[string]Candidate{}
	for id, cand := range c.ByNode {
		out.ByNode[id] = cand
	}
	for st, cand := range c.ByStepType {
		out.ByStepType[st] = cand
	}
	return out
}

// validate refuses a binding that would recompute a node whose outcome is
// not a deterministic function of recorded inputs, so an effectful node can
// never be "recomputed" into producing an effect.
func (c Candidates) validate(plan *workflow.CompiledWorkflow) error {
	for id, cand := range c.ByNode {
		node, ok := plan.Node(id)
		switch {
		case cand == nil:
			return refuse(CodeInvalidOptions, id, "candidate binding is nil")
		case !ok:
			return refuse(CodeInvalidOptions, id, "candidate bound to a node the plan does not declare")
		case !Recomputable(node):
			return refuse(CodeInvalidOptions, id,
				"%s with effect class %s is not recomputable; it replays its recorded outcome", node.Type, node.EffectClass)
		}
	}
	for st, cand := range c.ByStepType {
		probe := workflow.CompiledNode{Type: st, EffectClass: capability.EffectPure}
		if cand == nil || !Recomputable(probe) {
			return refuse(CodeInvalidOptions, "", "step type %s cannot carry a candidate", st)
		}
	}
	return nil
}

// lookup returns the candidate for a node, and only for a recomputable one: a
// mutating CAPABILITY matched by step type still replays its recorded outcome.
func (c Candidates) lookup(node workflow.CompiledNode) (Candidate, bool) {
	if !Recomputable(node) {
		return nil, false
	}
	if cand, ok := c.ByNode[node.ID]; ok {
		return cand, true
	}
	cand, ok := c.ByStepType[node.Type]
	return cand, ok
}
