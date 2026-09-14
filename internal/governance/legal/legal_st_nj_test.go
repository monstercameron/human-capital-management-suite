package legal_test

// LEGAL-ST-NJ-001: New Jersey's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from new-jersey.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-nj/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_NJ_001(t *testing.T) {
	legalStAssertPack(t, "NJ", []legalStExpectation{
		// Tiered state floor (6+ employees / ≤5 / agricultural)
		// (N.J.S.A. 34:11-56 family).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"34:11-56"}},
		// Wage-range-plus-benefits postings and internal-promotion
		// notices at 10+ employers (N.J.S.A. 34:6B-23).
		{Kind: legal.ObligationTypePayTransparency, Requires: []string{"34:6B-23", "internal_promotion"}},
		// Salary-history ban (N.J.S.A. 34:6B-20).
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"34:6B-20", "salary_history"}},
		// Written notice at hire and in advance of wage/payday/deduction
		// changes; Wage Theft Act 6-year SOL (N.J.S.A. 34:11-4.1 et seq.).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"34:11-4.1", "BEFORE"}},
		// Earned sick leave 1hr/30hrs, 40-hour cap (N.J.S.A. 34:11D-1)
		// plus family-leave/TDI insurance.
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"34:11D-1"}},
		// Diane B. Allen Equal Pay Act: substantially-similar-work
		// standard across protected classes (N.J.S.A. 10:5-12).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"10:5-12", "substantially similar"}},
		// NJ WARN: 90-day notice, mandatory severance plus a 4-week
		// inadequate-notice add-on (N.J.S.A. 34:21-1 et seq.).
		{Kind: legal.ObligationTypeMiniWARN, Requires: []string{"34:21-1", `"notice_days":90`}},
	})
}

func TestTodo_LEGAL_ST_NJ_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "NJ", nil)
	legalStGoldens(t, "NJ", p, release, registry)
}

func TestTodo_LEGAL_ST_NJ_001_Conformance(t *testing.T) {
	legalStConformance(t, "NJ", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_NJ_001_Mutation(t *testing.T) {
	legalStMutation(t, "NJ", nil)
}
