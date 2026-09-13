package legal_test

// LEGAL-ST-MA-001: Massachusetts's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from massachusetts.md and
// published through the section 7.1 pipeline at the status it honestly
// earns; the section 8.1 artifacts live under testdata/legal/us-ma/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_MA_001(t *testing.T) {
	legalStAssertPack(t, "MA", []legalStExpectation{
		// State minimum $15.00/hr (M.G.L. c. 151 § 1).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"15.00"}},
		// Pay-range posting at 25+ employers and pay-data reporting at
		// 100+ (M.G.L. c. 149 § 105F).
		{Kind: legal.ObligationTypePayTransparency, Requires: []string{"105F", "internal_promotion"}},
		// Salary-history ban (M.G.L. c. 149 § 105A).
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"105A", "salary_history"}},
		// 40-hour paid sick time plus PFML contributions
		// (c. 149 § 148 family / c. 175M).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"paid"}},
		// Weekly/biweekly pay frequency (M.G.L. c. 149 § 148).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"148", "WEEKLY"}},
		// Same-day on discharge; treble-damages Wage Act exposure
		// (c. 149 §§ 148-150).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"150", "Treble"}},
		// Mandatory garden leave at 50% of salary
		// (M.G.L. c. 149 § 24L).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"24L"}},
		// Negative-information notice and post-termination retention
		// (c. 149 § 52C).
		{Kind: legal.ObligationTypePersonnelFile, Requires: []string{"52C", `"response_days":10`}},
		// Personnel-record retention tied to § 52C.
		{Kind: legal.ObligationTypeRetention, Requires: []string{"52C", `"duration_years":3`}},
	})
}

func TestTodo_LEGAL_ST_MA_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "MA", nil)
	legalStGoldens(t, "MA", p, release, registry)
}

func TestTodo_LEGAL_ST_MA_001_Conformance(t *testing.T) {
	legalStConformance(t, "MA", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_MA_001_Mutation(t *testing.T) {
	legalStMutation(t, "MA", nil)
}
