package workspace

import (
	"context"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/workspacecontract"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// ApprovalTimeline is the approval half of the workspace: the chronology one
// zero-effect walk of the promote-into-management reference workflow
// produced, plus the receipt that proves the walk changed nothing.
type ApprovalTimeline struct {
	Events []contract.TimelineEvent
	// PlanDigest and ReceiptDigest identify the compiled plan that was walked
	// and the receipt the walk minted, so the timeline can be replayed rather
	// than believed.
	PlanDigest    string
	ReceiptDigest string
	// Terminal is the terminal code the walk reached.
	Terminal string
	// Outstanding names the obligations the terminal declared.
	Outstanding []string
}

// approvalTimeline walks the frozen promote-into-management reference
// workflow (internal/workflow/simulate) retargeted at this request, and
// projects its receipt into the contract's timeline shape.
//
// The reference plan is used rather than a plan built here because P1A
// compiles exactly one promotion workflow and this package owns no workflow
// authoring: what the workspace contributes is the proposal the plan is
// walked over, which is why the environment's worker, target, baseline and
// effective date are retargeted while the plan, the ports, the rule tables
// and the human-work directory stay exactly as the workflow lane published
// them.
//
// The walk is always SIMULATE: simulate.Run refuses any handler that is not
// zero-effect, and the receipt it mints carries the effect counters that make
// "nothing happened" checkable rather than asserted.
func approvalTimeline(ctx context.Context, req Request, worker values.EntityRef, proposedBase string) (ApprovalTimeline, error) {
	setup, err := simulate.NewPromotionSetup(proposedBase)
	if err != nil {
		return ApprovalTimeline{}, fmt.Errorf("workspace: wire the promotion reference workflow: %w", err)
	}

	env := setup.Env
	env.Worker = worker
	env.Target = req.Target
	env.EffectiveDate = req.EffectiveDate
	env.EvaluationDate = req.EvaluationDate
	env.BusinessReason = req.BusinessReason
	env.Current = req.Current
	env.Annualization = req.Annualization
	env.Policy = req.Policy
	if req.Budget != nil {
		env.Budget = req.Budget
	}

	base, ok := req.Proposed.Base.Get()
	if !ok {
		return ApprovalTimeline{}, fmt.Errorf("%w: the proposed base pay is %s",
			ErrQueryInvalid, req.Proposed.Base.State())
	}
	setup.Inputs.Values["worker_id"] = simulate.NewBranded("WorkerID", worker.Id)
	setup.Inputs.Values["target_job_id"] = simulate.NewBranded("JobID", req.Target.JobCode)
	setup.Inputs.Values["proposed_base_pay"] = simulate.NewMoney(base)
	setup.Inputs.Values["effective_date"] = simulate.NewLocalDate(req.EffectiveDate)

	receipt, err := simulate.Run(ctx, setup.Plan, setup.Inputs, setup.Options)
	if err != nil {
		return ApprovalTimeline{}, fmt.Errorf("workspace: simulate the promotion workflow: %w", err)
	}

	out := ApprovalTimeline{
		PlanDigest:    receipt.PlanDigest,
		ReceiptDigest: receipt.Digest(),
		Terminal:      receipt.Terminal.TerminalCode,
		Outstanding:   receipt.Terminal.OutstandingObligationRefs,
	}
	for _, node := range receipt.Trace {
		out.Events = append(out.Events, contract.TimelineEvent{
			At:    node.EnteredAt,
			Actor: string(node.Type),
			Label: fmt.Sprintf("%s: %s", node.NodeID, node.Detail),
		})
	}
	for _, item := range receipt.WorkItems {
		out.Events = append(out.Events, contract.TimelineEvent{
			At:    item.DecideBy,
			Actor: approverActor(item),
			Label: fmt.Sprintf("stage %d %s %s would be awaited for %s (tier %s, quorum %d)",
				item.Stage, item.Kind, strings.ToLower(string(item.State)),
				item.RequirementID, item.Tier, item.QuorumMin),
		})
	}
	return out, nil
}

// approverActor names who could decide one simulated work item.
//
// A candidate is not an approver and this never says it is: internal/humanwork
// resolved who is authorized to decide at the simulation instant, and
// decision-time authority is re-evaluated when a real decision lands. An
// empty candidate set is reported as such rather than left blank, because a
// requirement nobody can discharge is the finding.
func approverActor(item simulate.WorkItem) string {
	if len(item.Candidates) == 0 {
		return "no authorized approver (" + item.Outcome + ")"
	}
	return strings.Join(item.Candidates, "; ")
}
