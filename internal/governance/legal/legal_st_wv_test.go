package legal_test

// LEGAL-ST-WV-001: West Virginia's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from west-virginia.md and
// published through the section 7.1 pipeline at the status it honestly
// earns; the section 8.1 artifacts live under testdata/legal/us-wv/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_WV_001(t *testing.T) {
	legalStAssertPack(t, "WV", []legalStExpectation{
		// $8.75/hr for 6+-employee employers (W. Va. Code § 21-5C-2).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"8.75", "21-5C-2"}},
		// Written notice at hire plus 1-full-pay-period advance notice
		// of pay changes with mandatory reimbursement
		// (§ 21-5-9(1)-(4)).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"21-5-9", "BEFORE"}},
		// Semi-monthly, ≤19 days between paydays (§ 21-5-3).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"SEMIMONTHLY"}},
		// Next regular payday, 2x liquidated damages
		// (§ 21-5-4).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"21-5"}},
		// Sex-based comparable-character pay equity (§ 21-5B).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"21-5B"}},
		// Physician non-competes void (§ 47-11E-2).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"47-11E-2"}},
		// No private-sector personnel-file statutory right; the corpus
		// keeps the item RECOMMENDED (§ 21-3-22).
		{Kind: legal.ObligationTypePersonnelFile, Requires: []string{"21-3-22", "RECOMMENDED"}},
	})
}

func TestTodo_LEGAL_ST_WV_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "WV", nil)
	legalStGoldens(t, "WV", p, release, registry)
}

func TestTodo_LEGAL_ST_WV_001_Conformance(t *testing.T) {
	legalStConformance(t, "WV", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_WV_001_Mutation(t *testing.T) {
	legalStMutation(t, "WV", nil)
}
