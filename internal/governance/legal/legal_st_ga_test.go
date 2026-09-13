package legal_test

// LEGAL-ST-GA-001: Georgia's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from georgia.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-ga/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_GA_001(t *testing.T) {
	legalStAssertPack(t, "GA", []legalStExpectation{
		// WAGE_FLOOR is federal-only (the $5.15/hr state figure is
		// superseded by the FLSA): no state floor obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// Semi-monthly pay frequency (O.C.G.A. § 34-7-2).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"34-7-2", "SEMIMONTHLY"}},
		// Paid lactation breaks (O.C.G.A. § 34-1-6 family).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"34-4"}},
		// Restrictive Covenants Act, blue-pencil limits (O.C.G.A. § 13-8-50
		// et seq.).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"Separation"}},
		// Sex-based pay equity at 10+ employees with documentation duty
		// (O.C.G.A. §§ 34-5-1 to 34-5-7).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"34-5", `"documentation_required":true`}},
		// 4-year best-practice retention tied to § 34-4-5.
		{Kind: legal.ObligationTypeRetention, Requires: []string{"34-4-5", `"duration_years":4`}},
		// E-Verify mandatory at 11+ employees (O.C.G.A. § 13-10-91).
		{Kind: legal.ObligationTypeEVerify, Requires: []string{"13-10-91", `"employee_threshold":11`}},
		// State mass-separation notice DOL-402A within 48 hours.
		{Kind: legal.ObligationTypeSeparationFiling, Requires: []string{"DOL-402A"}},
	})
}

func TestTodo_LEGAL_ST_GA_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "GA", nil)
	legalStGoldens(t, "GA", p, release, registry)
}

func TestTodo_LEGAL_ST_GA_001_Conformance(t *testing.T) {
	legalStConformance(t, "GA", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_GA_001_Mutation(t *testing.T) {
	legalStMutation(t, "GA", nil)
}
