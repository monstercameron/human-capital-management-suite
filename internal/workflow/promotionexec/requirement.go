package promotionexec

import (
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/approverclass"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	FinanceApprovalPinnedPolicyRef = "policy.promotion.finance-partner/v1"
	ManagerApprovalPinnedPolicyRef = "policy.promotion.current-manager/v1"
	FinanceApprovalGovernanceRef   = "governance.promotion.finance-partner/v1"
	ManagerApprovalGovernanceRef   = "governance.promotion.current-manager/v1"
	FinanceApprovalAuthorityFloor  = "finance_partner"
	ManagerApprovalAuthorityFloor  = "current_manager"
)

func compileApprovalRequirement(id, approver string, decideBy time.Time, policyRef, governanceRef, authorityFloor string) (humanwork.ApprovalRequirement, error) {
	if approver == "" {
		return humanwork.ApprovalRequirement{}, fmt.Errorf("promotionexec: the approval requirement needs an approver principal")
	}
	if decideBy.IsZero() {
		return humanwork.ApprovalRequirement{}, fmt.Errorf("promotionexec: the approval requirement needs a decision deadline")
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
			RequesterMayNotApprove: true,
			SubjectMayNotApprove:   true,
			// Finance and current-manager approvals are independent authority
			// classes. A principal who already filled one requirement must not
			// be routed into the other, even when both expressions resolve them.
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

// CompileFinanceApprovalRequirement mirrors prototype.CompileApprovalRequirement
// for the Finance Partner approval.
func CompileFinanceApprovalRequirement(approver string, decideBy time.Time) (humanwork.ApprovalRequirement, error) {
	return compileApprovalRequirement(ApprovalFinance, approver, decideBy, FinanceApprovalPinnedPolicyRef, FinanceApprovalGovernanceRef, FinanceApprovalAuthorityFloor)
}

// CompileManagerApprovalRequirement mirrors prototype.CompileApprovalRequirement
// for the current-manager approval.
func CompileManagerApprovalRequirement(approver string, decideBy time.Time) (humanwork.ApprovalRequirement, error) {
	return compileApprovalRequirement(ApprovalManager, approver, decideBy, ManagerApprovalPinnedPolicyRef, ManagerApprovalGovernanceRef, ManagerApprovalAuthorityFloor)
}

// FinanceApproverFor and ManagerApproverFor are PROMOUX-003's fix for RED's
// second clause: "the work items share an undifferentiated
// principal:promotion-approver owner". A composition that configures one
// base approver identity (as every production composition today does --
// real per-relationship resolution is a later contract) must still route
// its finance and manager approvals to provably distinct principals, never
// to the identical literal string twice. Both functions derive from the
// same base through [approverclass.DeriveDistinct], so a caller that always
// derives before compiling or routing can never observe the two approvals
// sharing an owner: the class name is baked into the derived identity, so
// FinanceApproverFor(x) and ManagerApproverFor(x) differ for every x.
//
// A caller must derive with both functions from the same base and route
// each approval's WorkItem to its own derived principal (candidate,
// ChosenOwner, claim and completion), not just at compile time -- otherwise
// the compiled requirement's pinned candidate and the WorkItem's recorded
// owner disagree, which [humanwork.Resolution.Authorizes] then refuses at
// completion for an unrelated reason.
func FinanceApproverFor(base string) (string, error) {
	return approverclass.DeriveDistinct(base, approverclass.FinancePartner)
}

// ManagerApproverFor is [FinanceApproverFor]'s sibling for the
// current-manager authority class.
func ManagerApproverFor(base string) (string, error) {
	return approverclass.DeriveDistinct(base, approverclass.CurrentManager)
}
