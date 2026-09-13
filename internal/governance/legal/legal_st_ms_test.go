package legal_test

// LEGAL-ST-MS-001: Mississippi's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from mississippi.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-ms/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_MS_001(t *testing.T) {
	legalStAssertPack(t, "MS", []legalStExpectation{
		// Federal-only floor (state's $5.15/hr figure preempted,
		// Miss. Code § 71-1-51): no state-floor obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// 50+-employee manufacturers/public-service corporations pay
		// bi-weekly or semi-monthly (Miss. Code § 71-1-35).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"71-1-35"}},
		// Sex-based pay equity at 5+ employees, private-lawsuit-only
		// enforcement (Miss. Code § 71-17-5).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"71-17-5", "sex"}},
		// E-Verify mandatory for all employers regardless of size
		// (Miss. Code § 71-11-3).
		{Kind: legal.ObligationTypeEVerify, Requires: []string{"71-11-3"}},
		// Breach notification in the most expedient time possible, no
		// private right of action (Miss. Code § 75-24-29).
		{Kind: legal.ObligationTypeBreachNotification, Requires: []string{"75-24-29"}},
	})
}

func TestTodo_LEGAL_ST_MS_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "MS", nil)
	legalStGoldens(t, "MS", p, release, registry)
}

func TestTodo_LEGAL_ST_MS_001_Conformance(t *testing.T) {
	legalStConformance(t, "MS", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_MS_001_Mutation(t *testing.T) {
	legalStMutation(t, "MS", nil)
}
