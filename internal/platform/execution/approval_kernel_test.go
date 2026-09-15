package execution

import (
	"context"
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	intentapproval "github.com/monstercameron/human-capital-management-suite/internal/intent/approval"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

// TestTodo_WF_STEP_018_ApprovalKernelAdapter pins the served adapter between
// the journey's approval kernel port and the driver: the journey's recheck
// refusal travels with its typed cause, an admitted vote stands on the
// decision's own authority reference, and a vote the durable record no longer
// admits is reported as a decision conflict.
func TestTodo_WF_STEP_018_ApprovalKernelAdapter(t *testing.T) {
	var _ app.ApprovalKernel = executeDriverAdapter{}
	in := execute.CurrentApprovalAuthorityRequest{
		Item:     workitem.WorkItem{NodeID: "approve_finance"},
		Decision: intentapproval.ApprovalDecision{AuthorityDecisionRef: "authz:journey:intent-1"},
	}

	var seen workitem.WorkItem
	admitted, err := recheckAuthority(func(_ context.Context, _ workitem.Executor, item workitem.WorkItem) error {
		seen = item
		return nil
	}).Recheck(context.Background(), nil, in)
	if err != nil || !admitted.Allowed || admitted.DecisionRef != "authz:journey:intent-1" || seen.NodeID != "approve_finance" {
		t.Fatalf("admitted recheck = %+v, %v (saw %+v)", admitted, err, seen)
	}

	stale := errors.New("stale")
	refused, err := recheckAuthority(func(context.Context, workitem.Executor, workitem.WorkItem) error {
		return errors.Join(app.ErrProposalDecisionInvalidated, stale)
	}).Recheck(context.Background(), nil, in)
	if !errors.Is(err, app.ErrProposalDecisionInvalidated) || refused.Allowed {
		t.Fatalf("refused recheck = %+v, %v; want the journey's typed refusal", refused, err)
	}

	if nilRecheck, err := recheckAuthority(nil).Recheck(context.Background(), nil, in); err != nil || !nilRecheck.Allowed {
		t.Fatalf("absent recheck = %+v, %v; want the decision's own reference", nilRecheck, err)
	}

	if err := approvalKernelError(execute.ErrApprovalCompletionConflict); !errors.Is(err, app.ErrProposalDecisionConflict) || !errors.Is(err, execute.ErrApprovalCompletionConflict) {
		t.Fatalf("completion conflict = %v, want ErrProposalDecisionConflict keeping the cause", err)
	}
	other := errors.New("driver fault")
	if err := approvalKernelError(other); err != other {
		t.Fatalf("driver fault = %v, want it unchanged", err)
	}
}
