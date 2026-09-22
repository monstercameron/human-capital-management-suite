package execute

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	steptask "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/task"
)

// TaskResumePolicy binds the served TASK nodes to their certified contract
// (REV-008-01). The drift check in [checkWorkItemDrift] proves the durable
// row agrees with the request and the pinned plan, but it never consults
// internal/workflow/steps/task: a submission that fails schema validation or
// omits the accommodation acknowledgement, and even a typed outcome whose
// digest is not the stored completion's, would advance unchecked.
//
// When Tasks is set, the TASK resume path additionally:
//
//   - refuses a non-TASK work item parked on a TASK node, and a typed
//     outcome whose digest is not the stored row's own
//     CompletedOutputDigest (the tamper-evident binding Submit minted);
//   - loads the recorded TASK submission through LoadDecision (nil loads
//     through [workitem.LoadDecision] in the same transaction) and verifies
//     it through [steptask.Resolve] against the compiled node NodeFor maps
//     and the schema Validator, refusing before the advance commits.
//
// A completed TASK item with no recorded submission predates decision
// recording (the journey completion path mints none yet): it keeps the
// legacy drift-checked behavior so the served reapproval flow is not
// stranded. Recording a submission for such an item -- which requires the
// journey surface to collect the accommodation acknowledgement first --
// activates the full gate with no code change here.
type TaskResumePolicy struct {
	// NodeFor maps a pinned plan TASK node (and the stored item bound to
	// it) to the compiled TASK contract a submission must evidence. An
	// error refuses the resume: an unverifiable TASK node never advances.
	NodeFor func(selection runtime.WorkflowSelection, node workflow.CompiledNode, item workitem.WorkItem) (steptask.CompiledTaskNode, error)
	// Validator checks a recorded submission against its compiled node.
	// It must refuse a schema mismatch and a missing accessibility or
	// accommodation acknowledgement.
	Validator steptask.Validator
	// LoadDecision reads the recorded TASK submission. Nil uses
	// [workitem.LoadDecision] in the advancement's own transaction.
	LoadDecision func(ctx context.Context, ex workitem.Executor, tenantID, workItemID uuid.UUID) (workitem.DecisionRecord, error)
}

// checkTaskResume enforces [TaskResumePolicy] over a drift-checked TASK
// resume. outcome and refs are the drift check's own values; the returned
// outcome keeps their routing and binds the stored completion digest.
func (d *Driver) checkTaskResume(
	ctx context.Context,
	ex workitem.Executor,
	req ResumeRequest,
	selection runtime.WorkflowSelection,
	node workflow.CompiledNode,
	item workitem.WorkItem,
	outcome frontier.NodeOutcome,
	refs runtime.GovernanceRefs,
	at values.Instant,
) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	pol := d.opts.Tasks
	if pol == nil {
		return outcome, refs, nil
	}
	if item.Kind != workitem.KindTask {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, drift(
			"work item %s is kind %s, which cannot resume TASK node %s", item.WorkItemID, item.Kind, node.ID)
	}
	if outcome.OutputDigest != item.CompletedOutputDigest {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, drift(
			"typed outcome digest for work item %s does not match its stored completion", item.WorkItemID)
	}
	if pol.NodeFor == nil || pol.Validator == nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid(
			"task resume policy names no compiled node mapping or submission validator")
	}
	contract, err := pol.NodeFor(selection, node, item)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, drift(
			"work item %s names a TASK node with no verifiable contract: %v", item.WorkItemID, err)
	}
	load := pol.LoadDecision
	if load == nil {
		load = workitem.LoadDecision
	}
	rec, err := load(ctx, ex, req.Start.TenantID, req.WorkItemID)
	if err != nil {
		if workitem.CodeOf(err) == workitem.CodeWorkItemNotFound {
			// Legacy completion: no recorded submission exists, so there
			// is nothing for the task contract to verify. The drift
			// checks above still bind the outcome to the stored row.
			return outcome, refs, nil
		}
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	if rec.WorkItemID != req.WorkItemID || rec.WorkflowInstanceID != req.InstanceID {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, drift(
			"recorded decision for work item %s is bound to another work item or instance", item.WorkItemID)
	}
	if rec.Kind != workitem.DecisionKindTask {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, drift(
			"recorded decision for work item %s is kind %s, not a TASK submission", item.WorkItemID, rec.Kind)
	}
	if rec.BodyDigest != item.CompletedOutputDigest {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, drift(
			"recorded submission digest for work item %s does not match its stored completion", item.WorkItemID)
	}
	sub, err := steptask.DecodeSubmission(rec.Body)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("workflow execute: verify recorded TASK submission: %w", err)
	}
	if sub.Digest() != rec.BodyDigest {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, drift(
			"recorded submission for work item %s fails its own tamper evidence", item.WorkItemID)
	}
	continuation, err := steptask.NewContinuation(req.InstanceID, contract, item)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, drift(
			"work item %s does not bind its TASK contract: %v", item.WorkItemID, err)
	}
	if _, err := steptask.Resolve(continuation, item, &sub, pol.Validator, at, steptask.Event{}); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, fmt.Errorf("workflow execute: recorded TASK submission refused: %w", err)
	}
	outcome.OutputDigest = item.CompletedOutputDigest
	return outcome, refs, nil
}
