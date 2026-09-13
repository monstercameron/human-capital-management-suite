package legal_test

// LEGAL-ST-UT-001: Utah's promotion/base-pay obligation parameters carried
// in a reviewed RulePack generated from utah.md and published through the
// section 7.1 pipeline at the status it honestly earns; the section 8.1
// artifacts live under testdata/legal/us-ut/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_UT_001(t *testing.T) {
	legalStAssertPack(t, "UT", []legalStExpectation{
		// Federal-only floor: no state-floor obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// Semimonthly or more frequent; monthly for salaried
		// (§ 34-28-3).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"34-28-3"}},
		// 24 hours on involuntary discharge, next payday on resignation,
		// 60-day willful-withholding accumulation (§ 34-28-5).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"34-28-5"}},
		// Flat 1-year post-employment cap, unconditional
		// (§ 34-51-102).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"34-51-102"}},
		// Comparable-work pay equity (§ 34A-5-106(1)(a)).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"34A-5-106"}},
		// E-Verify mandatory at 150+ employees (§ 13-47-101).
		{Kind: legal.ObligationTypeEVerify, Requires: []string{"13-47-101", `"employee_threshold":150`}},
		// 1-year wage-record retention (§ 34-28-10).
		{Kind: legal.ObligationTypeRetention, Requires: []string{"34-28-10", `"duration_years":1`}},
	})
}

func TestTodo_LEGAL_ST_UT_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "UT", nil)
	legalStGoldens(t, "UT", p, release, registry)
}

func TestTodo_LEGAL_ST_UT_001_Conformance(t *testing.T) {
	legalStConformance(t, "UT", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_UT_001_Mutation(t *testing.T) {
	legalStMutation(t, "UT", nil)
}
