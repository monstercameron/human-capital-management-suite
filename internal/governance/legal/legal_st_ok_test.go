package legal_test

// LEGAL-ST-OK-001: Oklahoma's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from oklahoma.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-ok/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_OK_001(t *testing.T) {
	legalStAssertPack(t, "OK", []legalStExpectation{
		// Federal-only floor (State Question 832 rejected June 2026,
		// 40 O.S. § 197.2): no state-floor obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// Semimonthly minimum, 11-day max period-to-payday gap
		// (40 O.S. § 165.2).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"165.2"}},
		// Next designated payday, 2%/day liquidated damages for willful
		// withholding (40 O.S. § 165.3).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"165.3"}},
		// Pure non-competes void; established-customer/employee
		// non-solicitation enforceable (15 O.S. §§ 219A, 219B).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"219A"}},
		// Written policy and 10-day employee notice for drug testing
		// (40 O.S. §§ 551-563; 63 O.S. § 427.8).
		{Kind: legal.ObligationTypeDrugTesting, Requires: []string{"427.8", `"written_policy_required":true`}},
		// LEAVE_INTERACTION is preempted at locality level statewide
		// (40 O.S. § 160): the pack records a PreemptionAssertion, not an
		// obligation.
		{Kind: legal.ObligationTypeLeaveInteraction, Absent: true},
	})
}

func TestTodo_LEGAL_ST_OK_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "OK", nil)
	legalStGoldens(t, "OK", p, release, registry)
}

func TestTodo_LEGAL_ST_OK_001_Conformance(t *testing.T) {
	legalStConformance(t, "OK", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_OK_001_Mutation(t *testing.T) {
	legalStMutation(t, "OK", nil)
}
