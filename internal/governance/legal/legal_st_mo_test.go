package legal_test

// LEGAL-ST-MO-001: Missouri's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from missouri.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-mo/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_MO_001(t *testing.T) {
	legalStAssertPack(t, "MO", []legalStExpectation{
		// State floor on the HB 567 post-indexing schedule
		// (RSMo § 290.502).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"290.502"}},
		// 30-day advance written notice before a pay-rate reduction
		// (RSMo § 290.100).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"written"}},
		// Semi-monthly/every-16-days frequency for corporations
		// (RSMo § 290.080).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"290.080"}},
		// Immediate on discharge, continuing-wages penalty
		// (RSMo § 290.110).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"290.110"}},
		// Proposition A was repealed 2025-08-28: no state leave
		// interaction remains.
		{Kind: legal.ObligationTypeLeaveInteraction, Absent: true},
		// Non-solicit safe harbor ≤1 year (RSMo § 431.202).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"431.202"}},
		// Sex-based pay equity (RSMo § 290.410).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"290.410"}},
	})
}

func TestTodo_LEGAL_ST_MO_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "MO", nil)
	legalStGoldens(t, "MO", p, release, registry)
}

func TestTodo_LEGAL_ST_MO_001_Conformance(t *testing.T) {
	legalStConformance(t, "MO", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_MO_001_Mutation(t *testing.T) {
	legalStMutation(t, "MO", nil)
}
