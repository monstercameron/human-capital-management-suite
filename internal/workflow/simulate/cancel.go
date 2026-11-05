package simulate

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// CancelSpec cancels a test run after it dispatches AtNodeID and unwinds
// what the run committed, following the compiled unwind plan. Contracts
// declares the reversal contract of every write node the run may commit;
// a committed write without a contract refuses the cancel rather than
// unwinding by guess. A nil CancelSpec runs to completion.
type CancelSpec struct {
	// AtNodeID is the node the run dispatches last. Nodes after it never
	// run; a node the walk never reaches refuses the run.
	AtNodeID string
	// Contracts carries the reversal contract, commit dependencies and
	// atomic region of each write node, keyed by node id.
	Contracts []workflow.WriteNode
}

func (s *CancelSpec) check(plan *workflow.CompiledWorkflow) error {
	if s == nil {
		return nil
	}
	if s.AtNodeID == "" {
		return refuse(CodeInvalidOptions, "", "a cancel names the node it cancels at")
	}
	if _, ok := plan.Node(s.AtNodeID); !ok {
		return refuse(CodeInvalidOptions, s.AtNodeID, "plan declares no cancel point %q", s.AtNodeID)
	}
	return nil
}

// CancelPrompt is one human decision the cancel strands: an open work item
// the run raised and nobody will now decide.
type CancelPrompt struct {
	NodeID        string `json:"node_id"`
	Kind          string `json:"kind"`
	RequirementID string `json:"requirement_id"`
}

// CancelRecord is the settlement a canceled test run mints: where it
// stopped, what it had committed, the unwind groups the compiled plan
// demands for those effects, and the human decisions left open.
type CancelRecord struct {
	AtNodeID  string                   `json:"at_node_id"`
	Committed []workflow.PlannedEffect `json:"committed,omitempty"`
	Unwind    []workflow.UnwindGroup   `json:"unwind,omitempty"`
	Prompts   []CancelPrompt           `json:"prompts,omitempty"`
}

// unwindForCancel computes the unwind for the effects a run committed,
// following workflow.CompileUnwindPlan: contracts filter to the committed
// set with dependencies pruned to it, one safe point per committed node,
// and the longest prefix wins, which is always the whole committed set.
func unwindForCancel(contracts []workflow.WriteNode, committedIDs []string, atNodeID string) ([]workflow.PlannedEffect, []workflow.UnwindGroup, error) {
	committed := map[string]bool{}
	var order []string
	for _, id := range committedIDs {
		if !committed[id] {
			committed[id] = true
			order = append(order, id)
		}
	}
	byNode := map[string]workflow.WriteNode{}
	for _, c := range contracts {
		byNode[c.NodeID] = c
	}
	var effective []workflow.WriteNode
	for _, id := range order {
		contract, ok := byNode[id]
		if !ok {
			return nil, nil, fmt.Errorf("%w: cancel at %s committed %s",
				workflow.ErrNoReversalContract, atNodeID, id)
		}
		pruned := contract
		pruned.After = nil
		for _, dep := range contract.After {
			if committed[dep] {
				pruned.After = append(pruned.After, dep)
			}
		}
		effective = append(effective, pruned)
	}
	if len(effective) == 0 {
		return nil, nil, nil
	}
	points := make([]workflow.SafePoint, 0, len(order))
	for _, id := range order {
		points = append(points, workflow.SafePoint{ID: "cancel:" + atNodeID + ":" + id, ThroughNode: id})
	}
	plan, err := workflow.CompileUnwindPlan(effective, points)
	if err != nil {
		return nil, nil, err
	}
	best := workflow.SafePointPlan{}
	for _, point := range plan.Points {
		if len(point.Committed) > len(best.Committed) {
			best = point
		}
	}
	if len(best.Committed) != len(order) {
		return nil, nil, refuse(CodeCancelPlanMismatch, atNodeID,
			"the compiled unwind covers %d of %d committed effects", len(best.Committed), len(order))
	}
	return best.Committed, best.Unwind, nil
}

// promptsForCancel projects the run's open work items to the human
// decisions the cancel strands.
func promptsForCancel(items []WorkItem) []CancelPrompt {
	var out []CancelPrompt
	for _, item := range items {
		out = append(out, CancelPrompt{NodeID: item.NodeID, Kind: item.Kind, RequirementID: item.RequirementID})
	}
	return out
}

// mintCancel mints the receipt for a run canceled after atNodeID: a
// CANCELLED pseudo-terminal (the run reached no compiled terminal
// artifact), the recorded effects, and the cancel settlement. It mirrors
// mint so a canceled receipt stays whole; fields a cancel cannot have
// stay zero rather than invented.
func (r *run) mintCancel(startedAt time.Time, atNodeID string) (Receipt, error) {
	committedIDs := make([]string, 0, len(r.recorded))
	for _, rec := range r.recorded {
		committedIDs = append(committedIDs, rec.NodeID)
	}
	var contracts []workflow.WriteNode
	if r.opts.Cancel != nil {
		contracts = r.opts.Cancel.Contracts
	}
	committed, unwind, err := unwindForCancel(contracts, committedIDs, atNodeID)
	if err != nil {
		return Receipt{}, err
	}
	endedAt := r.now()
	counters := evidence.ZeroEffects()
	receipt := Receipt{
		InterpreterVersion: InterpreterVersion,
		CompilerVersion:    r.plan.CompilerVersion,
		WorkflowID:         r.plan.WorkflowID,
		WorkflowVersion:    r.plan.Version,
		PlanDigest:         r.plan.Digest(),
		Mode:               workflow.ModeSimulate,
		TerminalProfile:    r.plan.TerminalProfile,
		InputsDigest:       r.inputsDigest(),
		StartedAt:          startedAt,
		EndedAt:            endedAt,
		Elapsed:            endedAt.Sub(startedAt),
		Trace:              r.trace,
		WorkItems:          r.workItems,
		Terminal: Terminal{
			NodeID:        atNodeID,
			TerminalCode:  "CANCELLED",
			RuntimeStatus: workflow.RuntimeCancelled,
		},
		// A canceled run reached no terminal artifact, so the lifecycle
		// carries only what the cancel itself declares: the kernel's
		// RequestCancelled. The zero-effect receipt demands a request
		// state; the other dimensions stay zero because no terminal
		// declared them.
		Lifecycle:       lifecycleStateOf(lifecycle.Dimensions{Request: lifecycle.RequestCancelled}),
		Counters:        countersOf(counters),
		Controls:        r.controls(),
		RecordedEffects: append([]RecordedEffect(nil), r.recorded...),
		Cancel: &CancelRecord{
			AtNodeID:  atNodeID,
			Committed: committed,
			Unwind:    unwind,
			Prompts:   promptsForCancel(r.workItems),
		},
	}
	if receipt.WorkItems == nil {
		receipt.WorkItems = []WorkItem{}
	}
	if receipt.Trace == nil {
		receipt.Trace = []NodeTrace{}
	}

	resultDigest := canonicalDigest(resultDigestProfile, struct {
		Trace           []NodeTrace      `json:"trace"`
		WorkItems       []WorkItem       `json:"work_items"`
		Terminal        Terminal         `json:"terminal"`
		Lifecycle       LifecycleState   `json:"lifecycle"`
		RecordedEffects []RecordedEffect `json:"recorded_effects,omitempty"`
		Cancel          *CancelRecord    `json:"cancel,omitempty"`
	}{receipt.Trace, receipt.WorkItems, receipt.Terminal, receipt.Lifecycle, receipt.RecordedEffects, receipt.Cancel})

	controls := make([]evidence.ControlVersion, 0, len(receipt.Controls))
	for _, c := range receipt.Controls {
		controls = append(controls, evidence.ControlVersion{Name: c.Name, Version: c.Version})
	}
	zero, err := evidence.NewZeroEffectReceipt(
		r.plan.WorkflowID,
		planVersionString(r.plan.Version),
		evidence.ModeSimulate,
		receipt.Lifecycle.RequestState,
		controls,
		receipt.InputsDigest,
		resultDigest,
		counters,
	)
	if err != nil {
		return Receipt{}, wrap(CodeReceiptInvalid, atNodeID, err,
			"zero-effect receipt refused")
	}
	receipt.ZeroEffect = zero
	receipt.ZeroEffectDigest = canonicalDigest(receiptDigestProfile+"#zero", string(zero.Canonical()))
	receipt.digest = computeReceiptDigest(receipt)
	return receipt, nil
}
