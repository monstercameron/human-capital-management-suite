package legal_test

// LEGAL-ST-MI-001: Michigan's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from michigan.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-mi/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_MI_001(t *testing.T) {
	legalStAssertPack(t, "MI", []legalStExpectation{
		// $13.73/hr (2026) on the Improved Workforce Opportunity Wage Act
		// schedule, tipped wage phasing to 100% by 2030 (MCL 408.934a).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"13.73", "CPI"}},
		// Earned Sick Time Act accrual protection (72 hours paid at 11+,
		// paid-plus-unpaid split below), eff. 2025-10-01.
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"accrued paid sick leave"}},
		// Reasonableness test with blue-pencil (MCL 445.774a).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{}},
		// Whistleblowers' Protection Act (MCL 15.361 et seq.).
		{Kind: legal.ObligationTypeAntiRetaliation, Requires: []string{"workers_compensation_claim"}},
	})
}

func TestTodo_LEGAL_ST_MI_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "MI", nil)
	legalStGoldens(t, "MI", p, release, registry)
}

func TestTodo_LEGAL_ST_MI_001_Conformance(t *testing.T) {
	legalStConformance(t, "MI", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_MI_001_Mutation(t *testing.T) {
	legalStMutation(t, "MI", nil)
}
