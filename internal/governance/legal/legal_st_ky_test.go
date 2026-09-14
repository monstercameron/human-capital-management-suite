package legal_test

// LEGAL-ST-KY-001: Kentucky's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from kentucky.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-ky/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_KY_001(t *testing.T) {
	legalStAssertPack(t, "KY", []legalStExpectation{
		// Federal-only floor (KRS 337.275 at $7.25): no state-floor
		// obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// Semi-monthly minimum pay frequency (KRS 337.020).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"337.020", "SEMIMONTHLY"}},
		// LATER_OF next regular payday and termination-plus-14-days
		// (KRS 337.055).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"337.055", "14 days"}},
		// 4-year payroll-record minimum (KRS 337.320).
		{Kind: legal.ObligationTypeRetention, Requires: []string{"337.320", `"duration_years":4`}},
		// Sex-based comparable-work pay equity (KRS 337.420-337.433,
		// corpus binds the KRS 344 discrimination chapter).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"KRS 344"}},
		// Seventh-consecutive-day overtime premium carried as the state's
		// classification duty (KRS 337.285 family).
		{Kind: legal.ObligationTypeClassification, Requires: []string{"KRS 341"}},
	})
}

func TestTodo_LEGAL_ST_KY_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "KY", nil)
	legalStGoldens(t, "KY", p, release, registry)
}

func TestTodo_LEGAL_ST_KY_001_Conformance(t *testing.T) {
	legalStConformance(t, "KY", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_KY_001_Mutation(t *testing.T) {
	legalStMutation(t, "KY", nil)
}
