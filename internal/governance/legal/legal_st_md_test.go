package legal_test

// LEGAL-ST-MD-001: Maryland's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from maryland.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-md/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_MD_001(t *testing.T) {
	legalStAssertPack(t, "MD", []legalStExpectation{
		// State floor plus the non-compete voidance threshold the corpus
		// binds the floor section to (Md. Lab. & Empl. § 3-716 family).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"22.50"}},
		// Written notice at least one pay period before a payday/rate
		// decrease (Md. Code, Lab. & Empl. § 3-504(a)).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"3-504", "BEFORE"}},
		// Wage-range-plus-benefits postings (§ 3-304.2) and salary-history
		// ban (§§ 3-304.1, 3-304.2).
		{Kind: legal.ObligationTypePayTransparency, Requires: []string{"3-304.2", "internal_promotion"}},
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"3-304.1", "salary_history"}},
		// Earned sick and safe leave accrual plus FAMLI paid leave
		// (§ 3-1301 et seq., Title 8.3).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"3-1301"}},
		// Non-compete void at or below 150% of the state minimum wage and
		// banned for healthcare workers under $350,000 (§ 3-716).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"3-716"}},
		// Mini-WARN: 60-day notice for 25%-or-15-worker reductions
		// (§ 11-301 et seq.).
		{Kind: legal.ObligationTypeMiniWARN, Requires: []string{"11-301", `"notice_days":60`}},
		// 3-year payroll-record retention (§ 3-424).
		{Kind: legal.ObligationTypeRetention, Requires: []string{"3-424", `"duration_years":3`}},
	})
}

func TestTodo_LEGAL_ST_MD_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "MD", nil)
	legalStGoldens(t, "MD", p, release, registry)
}

func TestTodo_LEGAL_ST_MD_001_Conformance(t *testing.T) {
	legalStConformance(t, "MD", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_MD_001_Mutation(t *testing.T) {
	legalStMutation(t, "MD", nil)
}
