package legal_test

// LEGAL-ST-AK-001: Alaska's promotion/base-pay obligation parameters carried
// in a reviewed RulePack generated from alaska.md and published through the
// section 7.1 pipeline at the status it honestly earns; the section 8.1
// artifacts live under testdata/legal/us-ak/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_AK_001(t *testing.T) {
	legalStAssertPack(t, "AK", []legalStExpectation{
		// $14.00/hr floor (AS § 23.10.065), CPI-indexed.
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"23.10.065", "14.00"}},
		// Change notice due on the payday before the time of change
		// (AS § 23.05.160).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"23.05.160", "BEFORE"}},
		// Semi-monthly pay-frequency minimum (AS § 23.05.140).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"23.05"}},
		// Final pay: 3 working days on discharge (AS § 23.05.140(f)-(g)).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"23.05.140", "3 working days"}},
		// Ballot Measure 1 paid sick leave (AS §§ 23.10.066-.067).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"23.10.066", "paid sick"}},
		// Employee/former-employee inspection right (AS § 23.10.430).
		{Kind: legal.ObligationTypePersonnelFile, Requires: []string{"23.10.430"}},
		// 3-year payroll-record retention (AS § 23.10.100).
		{Kind: legal.ObligationTypeRetention, Requires: []string{"23.10.100", `"duration_years":3`}},
	})
}

func TestTodo_LEGAL_ST_AK_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "AK", nil)
	legalStGoldens(t, "AK", p, release, registry)
}

func TestTodo_LEGAL_ST_AK_001_Conformance(t *testing.T) {
	legalStConformance(t, "AK", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_AK_001_Mutation(t *testing.T) {
	legalStMutation(t, "AK", nil)
}
