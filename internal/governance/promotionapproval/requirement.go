// Package promotionapproval owns the governance composition for promotion
// approval requirements. Workflow definitions consume this contract without
// depending on the human-work implementation package.
package promotionapproval

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/approverclass"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workitem"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const organizationScope = "acme/engineering"

type ApprovalRequirement = humanwork.ApprovalRequirement
type WorkItem = workitem.WorkItem

// Compile constructs the governed requirement for one promotion approval.
func Compile(id, approver string, decideBy time.Time, policyRef, governanceRef, authorityFloor string) (humanwork.ApprovalRequirement, error) {
	if approver == "" {
		return humanwork.ApprovalRequirement{}, fmt.Errorf("promotionapproval: the approval requirement needs an approver principal")
	}
	if decideBy.IsZero() {
		return humanwork.ApprovalRequirement{}, fmt.Errorf("promotionapproval: the approval requirement needs a decision deadline")
	}
	deadline := values.NewInstant(decideBy.UTC().Truncate(time.Second))
	return humanwork.Compile(humanwork.RequirementSpec{
		RequirementID: id,
		Revision:      1,
		Stage:         1,
		Candidates: humanwork.Named(approver, policyRef,
			humanwork.Scope{Kind: humanwork.ScopeOrganization, Ref: organizationScope}),
		AuthorityFloor: []string{authorityFloor},
		Quorum:         humanwork.Quorum{MinApprovals: 1},
		Deadline:       humanwork.Deadline{DecideBy: deadline, Expiry: deadline},
		Escalation: humanwork.EscalationPolicy{
			OnDeadline: humanwork.EscalationBlock,
			RuleID:     "rule.promotion.escalation.block/v1",
		},
		Separation: humanwork.SeparationConstraints{
			RequesterMayNotApprove:     true,
			SubjectMayNotApprove:       true,
			OneRequirementPerPrincipal: true,
			RuleID:                     "rule.promotion.separation/v1",
		},
		Invalidators: []humanwork.Invalidator{
			{Kind: humanwork.InvalidatorMaterialProposalChange, RuleID: "rule.promotion.invalidate.material_change/v1"},
			{Kind: humanwork.InvalidatorAuthorityRevoked, RuleID: "rule.promotion.invalidate.authority_revoked/v1"},
			{Kind: humanwork.InvalidatorDeadlineExpired, RuleID: "rule.promotion.invalidate.deadline/v1"},
		},
		Source: humanwork.RequirementSource{
			Tier:                rules.ApprovalTierStandard,
			TableID:             "promotion.approval.tier",
			TableVersion:        "1",
			TableDigest:         "sha256:promotion-approval-tier",
			MatchedRowID:        "promotion.standard",
			GovernancePolicyRef: governanceRef,
		},
	})
}

func DeriveDistinct(base string, class approverclass.Class) (string, error) {
	return approverclass.DeriveDistinct(base, class)
}

var (
	FinancePartner = approverclass.FinancePartner
	CurrentManager = approverclass.CurrentManager
)
