package legal_test

// LEGAL-ST-ME-001: Maine's promotion/base-pay obligation parameters carried
// in a reviewed RulePack generated from maine.md and published through the
// section 7.1 pipeline at the status it honestly earns; the section 8.1
// artifacts live under testdata/legal/us-me/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_ME_001(t *testing.T) {
	legalStAssertPack(t, "ME", []legalStExpectation{
		// CPI-indexed state floor with tip credit (26 M.R.S. §§ 664, 665).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"14.65", "664"}},
		// Written at-hire wage/hours/payday/leave-policy notice
		// (26 M.R.S. § 621-A).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"621-A", "written"}},
		// Salary-history ban (26 M.R.S. § 628-A).
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"628-A", "salary_history"}},
		// Weekly minimum pay frequency (26 M.R.S. § 621-A).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"621-A", "WEEKLY"}},
		// Next regular payday on separation (26 M.R.S. §§ 621-A, 626).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"621-A"}},
		// Earned Paid Leave accrual plus mandatory payout on termination
		// (26 M.R.S. §§ 637, 626 family).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"paid"}},
		// Non-compete void below ~$62,920/yr, 3-business-day pre-signing
		// notice (26 M.R.S. §§ 599-A, 599-B).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"599-A"}},
		// 6-year personnel-file retention with employee access
		// (26 M.R.S. §§ 630-A, 631).
		{Kind: legal.ObligationTypePersonnelFile, Requires: []string{"630-A"}},
		// Worksite-closure 90-day notice plus per-year severance
		// (26 M.R.S. § 635).
		{Kind: legal.ObligationTypeMiniWARN, Requires: []string{"635", `"notice_days":90`}},
		// 6-year payroll-record retention (26 M.R.S. § 630-A).
		{Kind: legal.ObligationTypeRetention, Requires: []string{"630-A", `"duration_years":6`}},
	})
}

func TestTodo_LEGAL_ST_ME_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "ME", nil)
	legalStGoldens(t, "ME", p, release, registry)
}

func TestTodo_LEGAL_ST_ME_001_Conformance(t *testing.T) {
	legalStConformance(t, "ME", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_ME_001_Mutation(t *testing.T) {
	legalStMutation(t, "ME", nil)
}
