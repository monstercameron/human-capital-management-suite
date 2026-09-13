package legal_test

// LEGAL-ST-MN-001: Minnesota's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from minnesota.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-mn/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_MN_001(t *testing.T) {
	legalStAssertPack(t, "MN", []legalStExpectation{
		// $11.41/hr statewide floor (Minn. Stat. §§ 177.24-177.25).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"11.41", "177.24"}},
		// Written notice before the effective date of any pay/basis/
		// payday/deduction change (§ 181.032).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"181.032", "BEFORE"}},
		// Salary-history ban (§ 363A.08) and pay-range posting at 30+
		// employers (§ 181.173).
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"363A.08", "salary_history"}},
		{Kind: legal.ObligationTypePayTransparency, Requires: []string{"181.173", "internal_promotion"}},
		// ESST 1hr/30hrs accrual surviving promotion, plus PFML
		// (§§ 181.9445-181.9448, ch. 268B).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"181"}},
		// 48-hour weekly overtime threshold, stricter than federal 40
		// (§§ 177.24-177.25).
		{Kind: legal.ObligationTypeClassification, Requires: []string{"OVERTIME_THRESHOLD"}},
		// Non-competes void except business-sale/dissolution, retroactive
		// from 2023-07-01 (§ 181.988).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"181.988"}},
		// Personnel-file inspection cadence and response window
		// (§§ 181.960-181.966 family; the corpus binds the item to the
		// § 177.25 overtime section it was extracted alongside).
		{Kind: legal.ObligationTypePersonnelFile, Requires: []string{"REQUIRED"}},
	})
}

func TestTodo_LEGAL_ST_MN_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "MN", nil)
	legalStGoldens(t, "MN", p, release, registry)
}

func TestTodo_LEGAL_ST_MN_001_Conformance(t *testing.T) {
	legalStConformance(t, "MN", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_MN_001_Mutation(t *testing.T) {
	legalStMutation(t, "MN", nil)
}
