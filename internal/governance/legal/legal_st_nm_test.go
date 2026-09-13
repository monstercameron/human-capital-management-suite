package legal_test

// LEGAL-ST-NM-001: New Mexico's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from new-mexico.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-nm/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_NM_001(t *testing.T) {
	legalStAssertPack(t, "NM", []legalStExpectation{
		// $12.00/hr statewide with Santa Fe/Albuquerque/Las Cruces/
		// Bernalillo County overlays (NMSA 1978 § 50-4-22).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"12.00", "50-4-22"}},
		// Semimonthly paydays ≤16 days apart (NMSA 1978 § 50-4-2).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"50-4-2"}},
		// Fixed wages within 5 days on discharge, next payday on
		// resignation (§§ 50-4-4, 50-4-5).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"50-4-4"}},
		// Healthy Workplaces Act accrual with 64-hour carryover cap
		// (§ 50-17-1 et seq.).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"50-17-1"}},
		// Healthcare-profession non-competes void, 1-year non-solicit
		// permitted (§ 50-4A-1).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"50-4A-1"}},
		// Ban-the-box anti-retaliation for private employers
		// (§ 28-2-3.1 family).
		{Kind: legal.ObligationTypeAntiRetaliation, Requires: []string{"BLOCK"}},
	})
}

func TestTodo_LEGAL_ST_NM_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "NM", nil)
	legalStGoldens(t, "NM", p, release, registry)
}

func TestTodo_LEGAL_ST_NM_001_Conformance(t *testing.T) {
	legalStConformance(t, "NM", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_NM_001_Mutation(t *testing.T) {
	legalStMutation(t, "NM", nil)
}
