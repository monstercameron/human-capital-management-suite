package legal_test

// LEGAL-ST-NE-001: Nebraska's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from nebraska.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-ne/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_NE_001(t *testing.T) {
	legalStAssertPack(t, "NE", []legalStExpectation{
		// $15.00/hr (2026) with the Initiative 433 percentage schedule
		// from 2027 (Neb. Rev. Stat. § 48-1203 family).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"15.00"}},
		// 30-day advance written notice for payday changes
		// (Neb. Rev. Stat. § 48-1230).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"48-1230", `"timing_days":30`}},
		// Immediate on separation, accrued PTO as wages
		// (Neb. Rev. Stat. § 48-1230).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"48-1230"}},
		// Paid sick leave accrual from 80 hours worked at 11+ employees
		// (Initiative 436, §§ 48-3801 et seq.).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"48-38"}},
		// Common-law non-competes only.
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{}},
		// State mini-WARN: 90-day notice for 25+-employee same-day
		// separations (LB 921).
		{Kind: legal.ObligationTypeMiniWARN, Requires: []string{"LB 921", `"notice_days":90`}},
	})
}

func TestTodo_LEGAL_ST_NE_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "NE", nil)
	legalStGoldens(t, "NE", p, release, registry)
}

func TestTodo_LEGAL_ST_NE_001_Conformance(t *testing.T) {
	legalStConformance(t, "NE", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_NE_001_Mutation(t *testing.T) {
	legalStMutation(t, "NE", nil)
}
