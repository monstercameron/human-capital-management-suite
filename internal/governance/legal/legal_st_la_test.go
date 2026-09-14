package legal_test

// LEGAL-ST-LA-001: Louisiana's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from louisiana.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-la/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_LA_001(t *testing.T) {
	legalStAssertPack(t, "LA", []legalStExpectation{
		// Federal-only floor (no state statute): no state-floor
		// obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// Earlier of 15 days or next regular payday, penalty interest
		// (La. R.S. 23:631-632).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"termination_any"}},
		// 2-year cap with reasonable scope/territory and a
		// legitimate-business-interest requirement (La. R.S. 23:921).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"noncompete"}},
		// No state-specific pay-equity statute (matrix cell F).
		{Kind: legal.ObligationTypePayEquityReview, Absent: true},
	})
}

func TestTodo_LEGAL_ST_LA_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "LA", nil)
	legalStGoldens(t, "LA", p, release, registry)
}

func TestTodo_LEGAL_ST_LA_001_Conformance(t *testing.T) {
	legalStConformance(t, "LA", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_LA_001_Mutation(t *testing.T) {
	legalStMutation(t, "LA", nil)
}
