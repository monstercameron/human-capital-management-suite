package legal_test

// LEGAL-ST-ID-001: Idaho's promotion/base-pay obligation parameters carried
// in a reviewed RulePack generated from idaho.md and published through the
// section 7.1 pipeline at the status it honestly earns; the section 8.1
// artifacts live under testdata/legal/us-id/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_ID_001(t *testing.T) {
	legalStAssertPack(t, "ID", []legalStExpectation{
		// $7.25/hr tracking the federal floor with a youth sub-minimum
		// (Idaho Code § 44-1502); the corpus binds the floor to § 45-606.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// Written wage-reduction notice before the affected work
		// (Idaho Code § 45-610).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"45-610"}},
		// Final pay: earlier of next regular payday or 10 calendar days
		// (Idaho Code § 45-606).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"45-606", "10"}},
		// Key-employee-only, 18-month presumption of reasonableness
		// (Idaho Code §§ 44-2701, 44-2704).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"44-2701"}},
		// 3-year employment-record retention (Idaho Code § 45-610).
		{Kind: legal.ObligationTypeRetention, Requires: []string{"45-610", `"duration_years":3`}},
		// Breach notification without unreasonable delay
		// (Idaho Code § 28-51-105).
		{Kind: legal.ObligationTypeBreachNotification, Requires: []string{"28-51-105"}},
	})
}

func TestTodo_LEGAL_ST_ID_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "ID", nil)
	legalStGoldens(t, "ID", p, release, registry)
}

func TestTodo_LEGAL_ST_ID_001_Conformance(t *testing.T) {
	legalStConformance(t, "ID", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_ID_001_Mutation(t *testing.T) {
	legalStMutation(t, "ID", nil)
}
