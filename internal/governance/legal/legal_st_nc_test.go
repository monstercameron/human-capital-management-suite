package legal_test

// LEGAL-ST-NC-001: North Carolina's promotion/base-pay obligation
// parameters carried in a reviewed RulePack generated from
// north-carolina.md and published through the section 7.1 pipeline at the
// status it honestly earns; the section 8.1 artifacts live under
// testdata/legal/us-nc/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_NC_001(t *testing.T) {
	legalStAssertPack(t, "NC", []legalStExpectation{
		// Federal-only floor: no state-floor obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// Written pay-rate/payday notice at hire plus advance notice
		// before a wage decrease (§ 95-25.13).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"95-25.13", "BEFORE"}},
		// No private-sector salary-history ban; the corpus keeps the
		// state-agency-only restriction VERIFY-marked.
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"salary_history"}},
		// REDA protects workers'-comp, OSHA and wage-and-hour
		// complainants (§ 95-240 et seq.).
		{Kind: legal.ObligationTypeAntiRetaliation, Requires: []string{"95-240", "workers_compensation_claim"}},
		// School-involvement leave and protective-order leave
		// (§§ 95-28.3, 50B-5.5).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"95-28.3"}},
		// Written, signed, reasonable time/territory, construed against
		// the drafter (§ 75-4).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{}},
		// E-Verify mandatory at 25+ employees (HB 36).
		{Kind: legal.ObligationTypeEVerify, Requires: []string{"HB 36", `"employee_threshold":25`}},
	})
}

func TestTodo_LEGAL_ST_NC_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "NC", nil)
	legalStGoldens(t, "NC", p, release, registry)
}

func TestTodo_LEGAL_ST_NC_001_Conformance(t *testing.T) {
	legalStConformance(t, "NC", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_NC_001_Mutation(t *testing.T) {
	legalStMutation(t, "NC", nil)
}
