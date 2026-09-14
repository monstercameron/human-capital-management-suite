package legal_test

// LEGAL-ST-CT-001: Connecticut's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from connecticut.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-ct/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_CT_001(t *testing.T) {
	legalStAssertPack(t, "CT", []legalStExpectation{
		// ECI-indexed state floor announced each October 15 (PA 19-4,
		// Conn. Gen. Stat. § 31-58).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"31-58"}},
		// Wage-range-plus-benefits in postings (Conn. Gen. Stat. § 31-74g).
		{Kind: legal.ObligationTypePayTransparency, Requires: []string{"31-74g", "internal_promotion"}},
		// Salary-history ban (Conn. Gen. Stat. § 31-40z).
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"31-40z", "salary_history"}},
		// Weekly pay frequency (Conn. Gen. Stat. § 31-71b).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"31-71b", "WEEKLY"}},
		// Next business day on discharge / next payday on layoff
		// (Conn. Gen. Stat. § 31-71c family, corpus binds § 31-71f).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"31-71f"}},
		// Paid sick leave phased toward all employers plus CTFMLA leave
		// (Conn. Gen. Stat. § 31-57y / §§ 31-51kk family).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"31-51"}},
		// 10-business-day legal-review window for non-competes
		// (Conn. Gen. Stat. § 31-49h).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"31-49h"}},
		// Reasonable-time inspection, twice-yearly cap (§ 31-48h).
		{Kind: legal.ObligationTypePersonnelFile, Requires: []string{"31-48h"}},
		// Mass-layoff severance trigger (Conn. Gen. Stat. § 31-51u).
		{Kind: legal.ObligationTypeMiniWARN, Requires: []string{"31-51u"}},
	})
}

func TestTodo_LEGAL_ST_CT_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "CT", nil)
	legalStGoldens(t, "CT", p, release, registry)
}

func TestTodo_LEGAL_ST_CT_001_Conformance(t *testing.T) {
	legalStConformance(t, "CT", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_CT_001_Mutation(t *testing.T) {
	legalStMutation(t, "CT", nil)
}
