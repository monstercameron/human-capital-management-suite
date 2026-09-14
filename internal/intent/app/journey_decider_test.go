package app

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// deciderFixture is one open finance approval ASSIGNED to "finance" and a
// completed manager approval of the same proposal decided by "manager", for
// an intent initiated by "proposer".
func deciderFixture() ([]workitem.WorkItem, workitem.WorkItem, intent.Instance, time.Time) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	open := workitem.WorkItem{
		WorkItemID: uuid.MustParse("00000000-0000-4000-8000-0000000000f1"), Kind: workitem.KindApproval,
		NodeID: promotionexec.NodeApproveFinance, Status: workitem.StatusAssigned, ProposalRef: "sha256:proposal",
		ApprovalRequirementRef: promotionexec.ApprovalFinance, OwnerKind: workitem.OwnerPrincipal, OwnerRef: "finance",
		Assignment: workitem.Assignment{Resolution: humanwork.Resolution{Candidates: []humanwork.Candidate{{PrincipalID: "finance", Via: humanwork.SourceDirect}}}},
	}
	sibling := workitem.WorkItem{
		WorkItemID: uuid.MustParse("00000000-0000-4000-8000-0000000000f2"), Kind: workitem.KindApproval,
		NodeID: promotionexec.NodeApproveManager, Status: workitem.StatusCompleted, ProposalRef: "sha256:proposal",
		ApprovalRequirementRef: promotionexec.ApprovalManager, CompletedBy: "manager",
	}
	inst := intent.Instance{IntentID: "intent-decider", Initiator: intent.PrincipalReference{PrincipalID: "proposer"}}
	return []workitem.WorkItem{sibling, open}, open, inst, now
}

// TestJourneyDeciderAdmitsOnlyAnEligibleMemberWhoIsNeitherInitiatorNorSiblingDecider
// pins PROMOUX-015's decision admission. Each refusal case's fixture makes the
// refused principal a genuine member of the open item, so the refusal can only
// come from the guard the case names.
func TestJourneyDeciderAdmitsOnlyAnEligibleMemberWhoIsNeitherInitiatorNorSiblingDecider(t *testing.T) {
	t.Run("the assignee is admitted with its recorded candidate", func(t *testing.T) {
		items, open, inst, now := deciderFixture()
		candidate, err := journeyDecider(items, open, inst, "finance", now)
		if err != nil || candidate.PrincipalID != "finance" || candidate.Via != humanwork.SourceDirect {
			t.Fatalf("journeyDecider(assignee) = %+v, %v", candidate, err)
		}
	})
	t.Run("an eligible candidate of an available item is admitted", func(t *testing.T) {
		items, open, inst, now := deciderFixture()
		open.Status, open.OwnerKind, open.OwnerRef = workitem.StatusAvailable, workitem.OwnerCandidateSet, "candidates:set"
		open.Assignment.Resolution.Candidates = append(open.Assignment.Resolution.Candidates,
			humanwork.Candidate{PrincipalID: "delegate", Via: humanwork.SourceDelegated, DelegationID: "d1", DelegatedFrom: "finance", DelegationExpiry: values.NewInstant(now.Add(time.Hour))})
		items[1] = open
		candidate, err := journeyDecider(items, open, inst, "delegate", now)
		if err != nil || candidate.DelegationID != "d1" {
			t.Fatalf("journeyDecider(candidate) = %+v, %v", candidate, err)
		}
	})
	t.Run("a non-member is refused with the route sentinel", func(t *testing.T) {
		items, open, inst, now := deciderFixture()
		if _, err := journeyDecider(items, open, inst, "operator", now); !errors.Is(err, ErrProposalDecisionRoute) {
			t.Fatalf("journeyDecider(non-member) = %v, want ErrProposalDecisionRoute", err)
		}
		if _, err := journeyDecider(items, open, inst, "", now); !errors.Is(err, ErrProposalDecisionRoute) {
			t.Fatalf("journeyDecider(empty) = %v, want ErrProposalDecisionRoute", err)
		}
	})
	t.Run("an expired delegate is not an eligible candidate", func(t *testing.T) {
		items, open, inst, now := deciderFixture()
		open.Status, open.OwnerKind, open.OwnerRef = workitem.StatusAvailable, workitem.OwnerCandidateSet, "candidates:set"
		open.Assignment.Resolution.Candidates = []humanwork.Candidate{{PrincipalID: "delegate", Via: humanwork.SourceDelegated, DelegationID: "d1", DelegationExpiry: values.NewInstant(now.Add(-time.Hour))}}
		if _, err := journeyDecider(items, open, inst, "delegate", now); !errors.Is(err, ErrProposalDecisionRoute) {
			t.Fatalf("journeyDecider(expired delegate) = %v, want ErrProposalDecisionRoute", err)
		}
	})
	t.Run("an item another principal already claimed offers no claim", func(t *testing.T) {
		items, open, inst, now := deciderFixture()
		open.Status, open.ClaimedBy = workitem.StatusClaimed, "somebody"
		if _, err := journeyDecider(items, open, inst, "finance", now); !errors.Is(err, ErrProposalDecisionRoute) {
			t.Fatalf("journeyDecider(claimed) = %v, want ErrProposalDecisionRoute", err)
		}
	})
	t.Run("the initiator is refused even as the assignee", func(t *testing.T) {
		items, open, inst, now := deciderFixture()
		inst.Initiator.PrincipalID = "finance"
		if _, err := journeyDecider(items, open, inst, "finance", now); !errors.Is(err, ErrProposalDecisionSeparation) {
			t.Fatalf("journeyDecider(initiator) = %v, want ErrProposalDecisionSeparation", err)
		}
	})
	t.Run("the sibling approval's decider is refused even as the assignee", func(t *testing.T) {
		items, open, inst, now := deciderFixture()
		items[0].CompletedBy = "finance"
		if _, err := journeyDecider(items, open, inst, "finance", now); !errors.Is(err, ErrProposalDecisionSeparation) {
			t.Fatalf("journeyDecider(sibling decider) = %v, want ErrProposalDecisionSeparation", err)
		}
		// A completed approval of a different proposal is not a sibling.
		items[0].ProposalRef = "sha256:another-proposal"
		if _, err := journeyDecider(items, open, inst, "finance", now); err != nil {
			t.Fatalf("journeyDecider with another proposal's approval = %v, want admitted", err)
		}
	})
	t.Run("an assignee missing from the recorded candidates is refused", func(t *testing.T) {
		items, open, inst, now := deciderFixture()
		open.Assignment.Resolution.Candidates = nil
		if _, err := journeyDecider(items, open, inst, "finance", now); !errors.Is(err, ErrProposalDecisionRoute) {
			t.Fatalf("journeyDecider(uncandidated assignee) = %v, want ErrProposalDecisionRoute", err)
		}
	})
}

func TestRoutedApprovalRequirementRebuildsADelegatedCandidateFromItsDelegator(t *testing.T) {
	deadline := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	requirement, err := promotionexec.CompileManagerApprovalRequirement("manager", deadline)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	item := workitem.WorkItem{
		NodeID: promotionexec.NodeApproveManager, DeadlineAt: requirement.Deadline.Expiry.Time(),
		Assignment: workitem.Assignment{Resolution: humanwork.Resolution{
			RequirementDigest: requirement.Digest(),
			Candidates:        []humanwork.Candidate{{PrincipalID: "delegate", Via: humanwork.SourceDelegated, DelegatedFrom: "manager"}},
		}},
	}
	rebuilt, err := routedApprovalRequirement(item)
	if err != nil || rebuilt.Digest() != requirement.Digest() {
		t.Fatalf("routedApprovalRequirement(delegated) = %v, %v; want the delegator's requirement", rebuilt.Digest(), err)
	}
	item.Assignment.Resolution.Candidates[0].DelegatedFrom = ""
	if _, err := routedApprovalRequirement(item); err == nil {
		t.Fatal("a delegate with no delegator reproduced the manager's requirement digest")
	}
}

func TestJourneyDecisionErrorDeniesRouteAndSeparationRefusals(t *testing.T) {
	for _, cause := range []error{ErrProposalDecisionRoute, ErrProposalDecisionSeparation} {
		if err := journeyDecisionError(cause); !errors.Is(err, workspace.ErrDenied) || !errors.Is(err, cause) {
			t.Fatalf("journeyDecisionError(%v) = %v, want denied wrapping the cause", cause, err)
		}
	}
	if err := journeyDecisionError(ErrProposalDecisionStale); errors.Is(err, workspace.ErrDenied) || !errors.Is(err, ErrProposalDecisionStale) {
		t.Fatalf("journeyDecisionError(stale) = %v, want it unchanged", err)
	}
}

func TestAuthorizeDecisionAdmitsTheTypeWithoutTheExecutionRole(t *testing.T) {
	svc := &IntentService{executionAuthority: &ExecutionAuthority{AdmittedIntentTypes: map[string]bool{promotion.IntentType: true}, RequiredRole: "promotion_operator"}}
	if err := svc.authorizeDecision(promoteWorkerDefinition()); err != nil {
		t.Fatalf("authorizeDecision(admitted type) = %v, want nil: the execution role is not a decision's authority", err)
	}
	if err := (&IntentService{}).authorizeDecision(promoteWorkerDefinition()); err == nil {
		t.Fatal("authorizeDecision with no execution authority admitted the decision")
	}
}
