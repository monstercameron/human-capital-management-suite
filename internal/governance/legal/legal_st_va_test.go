package legal_test

// LEGAL-ST-VA-001: Virginia's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from virginia.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-va/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_VA_001(t *testing.T) {
	legalStAssertPack(t, "VA", []legalStExpectation{
		// Scheduled steps $12.77/hr (2026-2027) toward $15.00/hr, then
		// CPI (§ 40.1-28.10).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"12.77", "40.1-28.10"}},
		// Wage-range postings + salary-history ban eff. 2026-09-03 —
		// the window start this pack's boundary vectors pin
		// (§ 40.1-28.7:12).
		{Kind: legal.ObligationTypePayTransparency, Requires: []string{"40.1-28.7:12"}},
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"40.1-28.7:12", "salary_history"}},
		// Written notice for pay decreases only, 1+ pay period advance
		// (§ 40.1-29).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"40.1-29"}},
		// Non-competes void for low-wage/overtime-eligible employees and
		// healthcare professionals (§ 40.1-28.7:8, SB 1218).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"40.1-28.7:8"}},
		// Biweekly/semimonthly for hourly, monthly for salaried, treble
		// damages for knowing violations (§ 40.1-29).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"40.1-29"}},
		// Misclassification enforcement (§ 40.1-28.7:7).
		{Kind: legal.ObligationTypeClassification, Requires: []string{"40.1-28.7:7"}},
		// Home-health-worker-only sick leave is `L` scope in the
		// matrix, so no general leave interaction emits
		// (§ 40.1-33.3 et seq.).
		{Kind: legal.ObligationTypeLeaveInteraction, Absent: true},
	})
}

func TestTodo_LEGAL_ST_VA_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "VA", nil)
	legalStGoldens(t, "VA", p, release, registry)
}

func TestTodo_LEGAL_ST_VA_001_Conformance(t *testing.T) {
	legalStConformance(t, "VA", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_VA_001_Mutation(t *testing.T) {
	legalStMutation(t, "VA", nil)
}
