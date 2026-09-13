package legal_test

// LEGAL-ST-AR-001: Arkansas's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from arkansas.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-ar/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_AR_001(t *testing.T) {
	legalStAssertPack(t, "AR", []legalStExpectation{
		// $11.00/hr for 4+-employee employers (Ark. Code § 11-4-211).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"11.00", "11-4-211"}},
		// Semi-monthly minimum (Ark. Code § 11-4-401).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"11-4-401", "SEMIMONTHLY"}},
		// 7 days after employee demand or next regular payday, double-wage
		// penalty (Ark. Code § 11-4-405).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"11-4-405", "7 days"}},
		// Act 921 reasonableness test with mandatory blue-pencil reformation
		// (Ark. Code § 4-75-101).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"4-75-101"}},
		// Sex-based pay equity at any employer size (Ark. Code § 11-4-405).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"11-4-405", "sex"}},
	})
}

func TestTodo_LEGAL_ST_AR_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "AR", nil)
	legalStGoldens(t, "AR", p, release, registry)
}

func TestTodo_LEGAL_ST_AR_001_Conformance(t *testing.T) {
	legalStConformance(t, "AR", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_AR_001_Mutation(t *testing.T) {
	legalStMutation(t, "AR", nil)
}
