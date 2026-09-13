package legal_test

// LEGAL-ST-OR-001: Oregon's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from oregon.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-or/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_OR_001(t *testing.T) {
	legalStAssertPack(t, "OR", []legalStExpectation{
		// Three-tier floor: Portland metro $16.80 / standard $15.55 /
		// nonurban $14.55, CPI-indexed (the corpus carries the metro
		// figure on the state pack).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"16.80"}},
		// Salary-history ban with a voluntary-disclosure exception
		// (ORS 659A.357).
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"salary_history"}},
		// "Comparable character" pay-equity standard (ORS 652.220).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"652.220"}},
		// Oregon Sick Time 40 hrs/yr plus Paid Leave Oregon and the
		// SB 1515-narrowed OFLA (ORS 653.601-661, 657B).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"653.601"}},
		// Non-competes void below the indexed salary threshold, 2-week
		// pre-hire notice, 12-month cap, 50% garden leave
		// (ORS 653.295).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"653.295"}},
		// Ban-the-box until after the initial interview
		// (ORS 659A.360).
		{Kind: legal.ObligationTypeAntiRetaliation, Requires: []string{"659"}},
		// Final pay timing on termination (ORS 652.140).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"652.140"}},
	})
}

func TestTodo_LEGAL_ST_OR_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "OR", nil)
	legalStGoldens(t, "OR", p, release, registry)
}

func TestTodo_LEGAL_ST_OR_001_Conformance(t *testing.T) {
	legalStConformance(t, "OR", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_OR_001_Mutation(t *testing.T) {
	legalStMutation(t, "OR", nil)
}
