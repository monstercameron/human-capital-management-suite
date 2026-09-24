package promotionexec

import (
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/promotionapproval"
)

const (
	FinanceApprovalPinnedPolicyRef = "policy.promotion.finance-partner/v1"
	ManagerApprovalPinnedPolicyRef = "policy.promotion.current-manager/v1"
	FinanceApprovalGovernanceRef   = "governance.promotion.finance-partner/v1"
	ManagerApprovalGovernanceRef   = "governance.promotion.current-manager/v1"
	FinanceApprovalAuthorityFloor  = "finance_partner"
	ManagerApprovalAuthorityFloor  = "current_manager"
)

type ApprovalRequirement = promotionapproval.ApprovalRequirement

func compileApprovalRequirement(id, approver string, decideBy time.Time, policyRef, governanceRef, authorityFloor string) (ApprovalRequirement, error) {
	return promotionapproval.Compile(id, approver, decideBy, policyRef, governanceRef, authorityFloor)
}

// CompileFinanceApprovalRequirement mirrors prototype.CompileApprovalRequirement
// for the Finance Partner approval.
func CompileFinanceApprovalRequirement(approver string, decideBy time.Time) (ApprovalRequirement, error) {
	return compileApprovalRequirement(ApprovalFinance, approver, decideBy, FinanceApprovalPinnedPolicyRef, FinanceApprovalGovernanceRef, FinanceApprovalAuthorityFloor)
}

// CompileManagerApprovalRequirement mirrors prototype.CompileApprovalRequirement
// for the current-manager approval.
func CompileManagerApprovalRequirement(approver string, decideBy time.Time) (ApprovalRequirement, error) {
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
	return promotionapproval.DeriveDistinct(base, promotionapproval.FinancePartner)
}

// ManagerApproverFor is [FinanceApproverFor]'s sibling for the
// current-manager authority class.
func ManagerApproverFor(base string) (string, error) {
	return promotionapproval.DeriveDistinct(base, promotionapproval.CurrentManager)
}
