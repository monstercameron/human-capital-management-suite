package promotionexec

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	steptask "github.com/monstercameron/human-capital-management-suite/internal/workflow/steps/task"
)

// ReapprovalWorkType is the work type the execution layer routes for the
// served TASK node [NodeReapproval]. It is declared once here so the item
// factory, the completion path and the resume contract all name the same
// value instead of repeating the literal.
const ReapprovalWorkType = "task.promotion.reapproval/v1"

// The governed artifacts a reapproval submission must evidence. The form is
// the promotion reapproval's own; the accessibility and accommodation
// policies are the shared defaults the TASK step contract certifies
// (WF-STEP-004), versioned so a later policy revision is an explicit
// rebinding rather than a silent drift.
var (
	ReapprovalFormDefinition      = steptask.VersionedRef{Ref: "form.promotion.reapproval", Version: 1}
	ReapprovalAccessibilityPolicy = steptask.VersionedRef{Ref: "policy.accessibility.default", Version: 1}
	ReapprovalAccommodationPolicy = steptask.VersionedRef{Ref: "policy.accommodation.default", Version: 1}
)

// ReapprovalOutputSchema is the typed output the served TASK node declares.
// [Definition] builds the node from this same value, so the plan and the
// resume contract cannot disagree about what a reapproval produces.
func ReapprovalOutputSchema() workflow.SchemaRef {
	return schema("ReapprovalTaskResult")
}

// ReapprovalTaskContract maps a pinned plan TASK node and the stored item
// bound to it onto the certified TASK contract (REV-008-01). The workflow
// identity and output schema come from the pinned selection itself; the work
// type, form and policy pins are the canonical reapproval values above.
// Anything else -- another node, another work type, a plan whose output
// schema moved -- is refused so an unverifiable TASK node never advances.
//
// Its signature matches execute.TaskResumePolicy.NodeFor exactly, so the
// served composition names it directly instead of through an adapter.
func ReapprovalTaskContract(selection runtime.WorkflowSelection, planNode workflow.CompiledNode, item workitem.WorkItem) (steptask.CompiledTaskNode, error) {
	if planNode.ID != NodeReapproval {
		return steptask.CompiledTaskNode{}, fmt.Errorf("%w: node %q is not the served reapproval TASK", steptask.ErrBindingMismatch, planNode.ID)
	}
	if planNode.Type != workflow.StepTask {
		return steptask.CompiledTaskNode{}, fmt.Errorf("%w: node %q is not a TASK step", steptask.ErrBindingMismatch, planNode.ID)
	}
	if item.WorkType != ReapprovalWorkType || item.NodeID != NodeReapproval {
		return steptask.CompiledTaskNode{}, fmt.Errorf("%w: work item %s is not the served reapproval work", steptask.ErrBindingMismatch, item.WorkItemID)
	}
	want := ReapprovalOutputSchema()
	if planNode.OutputSchema != want {
		return steptask.CompiledTaskNode{}, fmt.Errorf("%w: plan output schema %s is not the served reapproval schema %s",
			steptask.ErrBindingMismatch, planNode.OutputSchema, want)
	}
	var planVersion uint32
	if selection.Plan != nil {
		planVersion = selection.Plan.Version
	}
	return steptask.CompiledTaskNode{
		WorkflowID: selection.WorkflowID, WorkflowVersion: planVersion,
		NodeID: NodeReapproval, WorkType: ReapprovalWorkType,
		OutputSchema:        planNode.OutputSchema,
		FormDefinition:      ReapprovalFormDefinition,
		AccessibilityPolicy: ReapprovalAccessibilityPolicy,
		AccommodationPolicy: ReapprovalAccommodationPolicy,
	}, nil
}

// ReapprovalTaskValidator is the schema Validator the served resume path
// runs over a recorded reapproval submission: the submission's output schema
// and form must equal the compiled node's, and the accessibility and
// accommodation acknowledgements must be present -- never assumed absent.
var ReapprovalTaskValidator = steptask.ValidatorFunc(func(req steptask.ValidationRequest) error {
	if req.Submission.OutputSchema != req.Node.OutputSchema {
		return fmt.Errorf("%w: submission schema %s is not the compiled schema %s",
			steptask.ErrValidationFailed, req.Submission.OutputSchema, req.Node.OutputSchema)
	}
	if req.Submission.FormDefinition != req.Node.FormDefinition {
		return fmt.Errorf("%w: submission form %+v is not the compiled form %+v",
			steptask.ErrValidationFailed, req.Submission.FormDefinition, req.Node.FormDefinition)
	}
	if req.Submission.AccessibilityEvidenceRef == "" || req.Submission.AccommodationEvidenceRef == "" {
		return fmt.Errorf("%w: accessibility and accommodation evidence are required", steptask.ErrValidationFailed)
	}
	return nil
})
