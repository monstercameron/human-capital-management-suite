package execute

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestTodo_WF_STEP_003_ClosedItemOutcomes pins which typed outcomes a WorkItem
// closed without a completion may carry into Resume (WF-STEP-003): an expired
// item only EXPIRED, a cancelled item only INVALIDATED or CANCELLED, an open
// item nothing, and a completed item whatever its resolution names.
func TestTodo_WF_STEP_003_ClosedItemOutcomes(t *testing.T) {
	outcomes := []workflow.Outcome{"APPROVED", workflow.OutcomeRejected, "INVALIDATED", "EXPIRED", "CANCELLED"}
	want := map[workitem.Status]map[workflow.Outcome]bool{
		workitem.StatusCompleted:  {"APPROVED": true, workflow.OutcomeRejected: true, "INVALIDATED": true, "EXPIRED": true, "CANCELLED": true},
		workitem.StatusExpired:    {"EXPIRED": true},
		workitem.StatusCancelled:  {"INVALIDATED": true, "CANCELLED": true},
		workitem.StatusAssigned:   {},
		workitem.StatusInProgress: {},
	}
	for status, allowed := range want {
		for _, outcome := range outcomes {
			if got := closedItemCarries(status, outcome); got != allowed[outcome] {
				t.Errorf("closedItemCarries(%s, %s) = %v, want %v", status, outcome, got, allowed[outcome])
			}
		}
	}
}

// TestTodo_WF_STEP_003_ResumeFromAClosedItem resumes the WF-RUN-028 fixture's
// WAITING approval from a second, durable approval item that was routed and
// then cancelled without a completion: a CANCELLED outcome takes the plan's
// CANCELLED terminal (through CANCELLING, in the advancement's own
// transaction), while an APPROVED outcome over the same closed row is refused
// as drift.
func TestTodo_WF_STEP_003_ResumeFromAClosedItem(t *testing.T) {
	f := newWfrun028Fixture(t, "wfstep003-closed")
	var closed workitem.WorkItem
	work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		item, err := workitem.NewApprovalTask(workitem.NewWorkItemInput{
			TenantID: f.tenantID, WorkType: "wfrun028.approval", CorrelationID: f.start.CorrelationID,
			WorkflowInstanceID: f.instanceID, NodeID: prototype.NodeApproval,
			ProposalRef: f.proposal.MaterialDigest.Digest, SubjectRefs: f.start.BusinessSubjectRefs,
			PolicyRouteRef: "route:wfrun028/v1", Visibility: workitem.VisibilityAssigneeOnly,
			OrganizationScopeID: "org:acme/people", DeadlineAt: f.at.Add(time.Hour), CreatedAt: f.at.Add(-time.Hour),
		}, prototype.ApprovalRequirementID)
		if err != nil {
			return err
		}
		store := workitem.Store{}
		if item, err = store.Create(context.Background(), tx, item, work006Meta("system:workflow", "created", f.at.Add(-time.Hour))); err != nil {
			return err
		}
		resolution := humanwork.Resolution{
			RequirementID: prototype.ApprovalRequirementID, RequirementRevision: 1, Outcome: humanwork.OutcomeResolved,
			Candidates: []humanwork.Candidate{{PrincipalID: "principal:approver", Via: humanwork.SourceDirect, TermRef: "role:approver"}},
			ResolvedAt: values.NewInstant(f.at.Add(-time.Hour)), EffectiveAt: values.NewInstant(f.at.Add(-time.Hour)),
			DirectoryVersion: "directory:1", ExpressionDigest: "sha256:" + strings.Repeat("c", 64), QuorumRequired: 1,
		}
		if item, err = store.Route(context.Background(), tx, f.tenantID, item.WorkItemID, item.ItemVersion,
			workitem.Assignment{Resolution: resolution, GovernancePolicyRef: "policy:wfrun028/v1", Trigger: workitem.TriggerInitialRouting, ChosenOwner: "principal:approver"},
			work006Meta("system:workflow", "routed", f.at.Add(-50*time.Minute))); err != nil {
			return err
		}
		closed, err = store.Cancel(context.Background(), tx, f.tenantID, item.WorkItemID, item.ItemVersion,
			work006Meta("principal:approver", "journey.approval.cancelled", f.at))
		return err
	})

	req := f.resumeRequest()
	req.WorkItemID, req.ExpectedWorkItemVersion = closed.WorkItemID, closed.ItemVersion

	approved := req
	approved.Outcome.Outcome = workflow.Outcome("APPROVED")
	if _, err := f.driver(t).Resume(context.Background(), approved); !errors.Is(err, ErrWorkItemDrift) {
		t.Fatalf("Resume(APPROVED from a cancelled item) = %v, want ErrWorkItemDrift", err)
	}

	cancelled := req
	cancelled.Outcome.Outcome = workflow.Outcome("CANCELLED")
	result, err := f.driver(t).Resume(context.Background(), cancelled)
	if err != nil {
		t.Fatalf("Resume(CANCELLED from a cancelled item): %v", err)
	}
	if result.Status != StatusComplete {
		t.Fatalf("result = %+v, want COMPLETE", result)
	}
	var inst runtime.Instance
	work006Tx(t, f.conn, f.tenantID, func(tx dbport.Tx) error {
		var loadErr error
		inst, loadErr = (runtime.Store{}).LoadInstance(context.Background(), tx, f.tenantID, f.instanceID)
		return loadErr
	})
	if inst.RuntimeStatus != runtime.InstanceCancelled {
		t.Fatalf("instance status = %s, want CANCELLED", inst.RuntimeStatus)
	}
}
