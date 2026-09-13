package legal_test

// LEGAL-ST-WI-001: Wisconsin's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from wisconsin.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-wi/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_WI_001(t *testing.T) {
	legalStAssertPack(t, "WI", []legalStExpectation{
		// Federal-only floor with statewide locality preemption
		// (§ 104.001(2) et al.): no state-floor obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// Next regular payday, or 24 hours on business-closure/merger/
		// relocation separation (§ 109.03).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"109.03"}},
		// Reasonableness test with blue-pencil (§ 103.465).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"103.465"}},
		// Twice-per-calendar-year access, 7-working-day response
		// (§ 103.13).
		{Kind: legal.ObligationTypePersonnelFile, Requires: []string{"103.13", `"response_days":7`}},
		// Mini-WARN 60-day notice for 25%-or-25-worker reductions
		// (§ 109.07).
		{Kind: legal.ObligationTypeMiniWARN, Requires: []string{"109.07"}},
		// Equal-pay protected bases (§§ 111.31-111.395).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"equal work"}},
		// LEAVE_INTERACTION/FIELD_RESTRICTION/PAY_TRANSPARENCY are
		// preempted at locality level statewide: PreemptionAssertions,
		// not obligations.
		{Kind: legal.ObligationTypeLeaveInteraction, Absent: true},
		{Kind: legal.ObligationTypeFieldRestriction, Absent: true},
		{Kind: legal.ObligationTypePayTransparency, Absent: true},
	})
}

func TestTodo_LEGAL_ST_WI_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "WI", nil)
	legalStGoldens(t, "WI", p, release, registry)
}

func TestTodo_LEGAL_ST_WI_001_Conformance(t *testing.T) {
	legalStConformance(t, "WI", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_WI_001_Mutation(t *testing.T) {
	legalStMutation(t, "WI", nil)
}
