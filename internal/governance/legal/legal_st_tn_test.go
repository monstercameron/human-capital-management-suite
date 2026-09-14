package legal_test

// LEGAL-ST-TN-001: Tennessee's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from tennessee.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-tn/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_TN_001(t *testing.T) {
	legalStAssertPack(t, "TN", []legalStExpectation{
		// Federal-only floor, preempted at locality level
		// (§ 50-2-112): no state-floor obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// At least monthly; semi-monthly wages due by the 20th of the
		// following month (§ 50-2-103).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"50-2-103"}},
		// LATER_OF next regular payday or 21 days after separation
		// (§ 50-2-103).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"50-2-103"}},
		// Pregnancy/adoption accommodation, 4-month leave for 15+
		// employers (§ 4-21-408 family; corpus binds § 50-2-112).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"50-2-112"}},
		// Common-law reasonableness, healthcare providers capped at
		// 2 years/10 miles (§ 63-1-148).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"63-1-148"}},
		// E-Verify at 35+-FTE private employers (§ 50-1-703).
		{Kind: legal.ObligationTypeEVerify, Requires: []string{"50-1-703", `"employee_threshold":35`}},
		// State plant-closure notification for 50-99-employee employers
		// (§ 50-1-601 et seq.).
		{Kind: legal.ObligationTypeMiniWARN, Requires: []string{"50-1-601"}},
	})
}

func TestTodo_LEGAL_ST_TN_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "TN", nil)
	legalStGoldens(t, "TN", p, release, registry)
}

func TestTodo_LEGAL_ST_TN_001_Conformance(t *testing.T) {
	legalStConformance(t, "TN", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_TN_001_Mutation(t *testing.T) {
	legalStMutation(t, "TN", nil)
}
