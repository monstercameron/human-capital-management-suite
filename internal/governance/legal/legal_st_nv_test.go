package legal_test

// LEGAL-ST-NV-001: Nevada's promotion/base-pay obligation parameters carried
// in a reviewed RulePack generated from nevada.md and published through the
// section 7.1 pipeline at the status it honestly earns; the section 8.1
// artifacts live under testdata/legal/us-nv/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_NV_001(t *testing.T) {
	legalStAssertPack(t, "NV", []legalStExpectation{
		// Flat $12.00/hr constitutional floor, no tip credit
		// (NRS 608.250).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"12.00", "608.250"}},
		// Wage-decrease notice 7 days before the reduced rate takes
		// effect (NRS 608.100).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"608.100", `"timing_days":7`}},
		// Salary-history ban and post-interview wage-range disclosure
		// (NRS 613.133, 613.330).
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"613.133", "salary_history"}},
		{Kind: legal.ObligationTypePayTransparency, Requires: []string{"613.133", "internal_promotion"}},
		// Semimonthly paydays on the 15th and last day (NRS 608.060).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"608.060", "SEMIMONTHLY"}},
		// Immediate on discharge, earlier of 7 days or next payday on
		// resignation (NRS 608.020, 608.030).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"termination_any"}},
		// 50+-employee paid-leave accrual surviving promotion
		// (NRS 608.0197).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"608.0197"}},
		// Non-competes unenforceable for hourly employees
		// (NRS 613.195).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"613.195"}},
		// Personnel-file inspection right (NRS 608.115, 613.075).
		{Kind: legal.ObligationTypePersonnelFile, Requires: []string{"613"}},
		// 2-year payroll-record retention (NRS 608.115).
		{Kind: legal.ObligationTypeRetention, Requires: []string{"608.115", `"duration_years":2`}},
	})
}

func TestTodo_LEGAL_ST_NV_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "NV", nil)
	legalStGoldens(t, "NV", p, release, registry)
}

func TestTodo_LEGAL_ST_NV_001_Conformance(t *testing.T) {
	legalStConformance(t, "NV", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_NV_001_Mutation(t *testing.T) {
	legalStMutation(t, "NV", nil)
}
