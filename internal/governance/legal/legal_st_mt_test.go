package legal_test

// LEGAL-ST-MT-001: Montana's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from montana.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-mt/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_MT_001(t *testing.T) {
	legalStAssertPack(t, "MT", []legalStExpectation{
		// Wrongful Discharge from Employment Act: good cause after
		// probation, grievance exhaustion with a 90-day safety valve
		// (MCA §§ 39-2-904, 39-2-912).
		{Kind: legal.ObligationTypeJobSecurity, Requires: []string{"39-2"}},
		// $10.85/hr, CPI-U-indexed, rounded to the nearest $0.05
		// (MCA § 39-3-404).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"10.85", "39-3-404"}},
		// Immediate on discharge; alleged-theft withholding carve-out
		// (MCA § 39-3-205).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"39-3-205"}},
		// Non-competes void except business-sale/goodwill/dissolution
		// (MCA § 28-2-703 et seq.).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"28-2-703"}},
		// No state-specific pay-equity statute (matrix cell F).
		{Kind: legal.ObligationTypePayEquityReview, Absent: true},
	})
}

func TestTodo_LEGAL_ST_MT_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "MT", nil)
	legalStGoldens(t, "MT", p, release, registry)
}

func TestTodo_LEGAL_ST_MT_001_Conformance(t *testing.T) {
	legalStConformance(t, "MT", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_MT_001_Mutation(t *testing.T) {
	legalStMutation(t, "MT", nil)
}
