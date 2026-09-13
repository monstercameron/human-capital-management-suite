package legal_test

// LEGAL-ST-SD-001: South Dakota's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from south-dakota.md and
// published through the section 7.1 pipeline at the status it honestly
// earns; the section 8.1 artifacts live under testdata/legal/us-sd/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_SD_001(t *testing.T) {
	legalStAssertPack(t, "SD", []legalStExpectation{
		// $11.85/hr (2026), CPI-U-indexed with a no-decrease floor,
		// tipped $5.925/hr (SDCL 60-11-3, 60-11-3.2).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"11.85"}},
		// Monthly or regular agreed payday communicated at hire
		// (SDCL 60-11-9).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"MONTHLY"}},
		// Next regular payday or reasonable time; property-return
		// withholding, 5-day written-demand payment (SDCL 60-11-10).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"termination_any"}},
		// 2-year non-compete cap within the employer's like-business
		// area; void for healthcare practitioners from 2023-07-01
		// (SDCL 53-9-11, 53-9-11.2).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"53-9-11"}},
		// "Comparable work" pay equity excluding physical strength
		// (SDCL 60-12-15).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{}},
		// Tobacco-use protection absent a bona fide occupational
		// requirement (SDCL 60-4-11).
		{Kind: legal.ObligationTypeAntiRetaliation, Requires: []string{}},
	})
}

func TestTodo_LEGAL_ST_SD_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "SD", nil)
	legalStGoldens(t, "SD", p, release, registry)
}

func TestTodo_LEGAL_ST_SD_001_Conformance(t *testing.T) {
	legalStConformance(t, "SD", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_SD_001_Mutation(t *testing.T) {
	legalStMutation(t, "SD", nil)
}
