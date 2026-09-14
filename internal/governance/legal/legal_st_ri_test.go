package legal_test

// LEGAL-ST-RI-001: Rhode Island's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from rhode-island.md and
// published through the section 7.1 pipeline at the status it honestly
// earns; the section 8.1 artifacts live under testdata/legal/us-ri/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_RI_001(t *testing.T) {
	legalStAssertPack(t, "RI", []legalStExpectation{
		// $16.00/hr (2026), CPI-adjusted toward $17.00 (2027)
		// (§ 28-12-3).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"16.00", "28-12-3"}},
		// Wage-range disclosure at hire/promotion/on-request and the
		// salary-history ban (Pay Equity Act, § 28-6-17 et seq.).
		{Kind: legal.ObligationTypePayTransparency, Requires: []string{"28-6-19", "internal_promotion"}},
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"28-6-19", "salary_history"}},
		// Weekly default frequency with bondable exceptions
		// (§ 28-14-2).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"28-14-2"}},
		// 40-hour paid sick/safe leave plus TDI/Caregiver Insurance and
		// PFMLA (§ 28-57-1 et seq., § 28-41-34, § 28-48-1 et seq.).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"28-57-1"}},
		// Non-competes void below $37,650/yr or for non-exempt workers
		// (§ 28-59).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"28-59"}},
		// Ban-the-box before first interview at 4+ employers
		// (§ 28-5-7(7) family).
		{Kind: legal.ObligationTypeAntiRetaliation, Requires: []string{"28-50"}},
	})
}

func TestTodo_LEGAL_ST_RI_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "RI", nil)
	legalStGoldens(t, "RI", p, release, registry)
}

func TestTodo_LEGAL_ST_RI_001_Conformance(t *testing.T) {
	legalStConformance(t, "RI", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_RI_001_Mutation(t *testing.T) {
	legalStMutation(t, "RI", nil)
}
