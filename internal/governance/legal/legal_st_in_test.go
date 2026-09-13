package legal_test

// LEGAL-ST-IN-001: Indiana's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from indiana.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-in/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_IN_001(t *testing.T) {
	legalStAssertPack(t, "IN", []legalStExpectation{
		// Federal-only floor (IC 22-2-2 at $7.25; locality preemption under
		// IC 22-2-2-10.5): no state-floor obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// Semimonthly or biweekly frequency (IC 22-2-5-1).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"SEMIMONTHLY"}},
		// Next regular payday on voluntary resignation (IC 22-2-5-2).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"termination_any"}},
		// 4-year payroll-record minimum (IC 22-2-8).
		{Kind: legal.ObligationTypeRetention, Requires: []string{`"duration_years":4`, "payroll_records"}},
		// Physician non-competes voided for hospital-system agreements
		// (IC 25-22.5-5.5, SEA 475).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"physician"}},
		// State separation notice filed with Indiana DOL.
		{Kind: legal.ObligationTypeSeparationFiling, Requires: []string{}},
	})
}

func TestTodo_LEGAL_ST_IN_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "IN", nil)
	legalStGoldens(t, "IN", p, release, registry)
}

func TestTodo_LEGAL_ST_IN_001_Conformance(t *testing.T) {
	legalStConformance(t, "IN", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_IN_001_Mutation(t *testing.T) {
	legalStMutation(t, "IN", nil)
}
