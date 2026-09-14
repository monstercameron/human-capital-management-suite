package legal_test

// LEGAL-ST-SC-001: South Carolina's promotion/base-pay obligation
// parameters carried in a reviewed RulePack generated from
// south-carolina.md and published through the section 7.1 pipeline at the
// status it honestly earns; the section 8.1 artifacts live under
// testdata/legal/us-sc/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_SC_001(t *testing.T) {
	legalStAssertPack(t, "SC", []legalStExpectation{
		// Federal-only floor: no state-floor obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// Written notice at hire plus 7-calendar-day advance notice for
		// changes (§ 41-10-30).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"41-10-30", `"timing_days":7`}},
		// Earlier of 48 hours or next regular payday, capped at 30 days
		// (§ 41-10-50).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"41-10-50"}},
		// HANDBOOK_DISCLAIMER job-security standard: a signed,
		// underlined-capital-letters disclaimer defeats implied-contract
		// claims (§ 41-1-110).
		{Kind: legal.ObligationTypeJobSecurity, Requires: []string{"41-1-110", "HANDBOOK_DISCLAIMER"}},
		// Common-law non-competes only, strictly construed, no
		// blue-pencil.
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{}},
		// E-Verify mandatory for all private employers within 3 business
		// days (§ 41-8-20(B)).
		{Kind: legal.ObligationTypeEVerify, Requires: []string{"41-8-20"}},
	})
}

func TestTodo_LEGAL_ST_SC_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "SC", nil)
	legalStGoldens(t, "SC", p, release, registry)
}

func TestTodo_LEGAL_ST_SC_001_Conformance(t *testing.T) {
	legalStConformance(t, "SC", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_SC_001_Mutation(t *testing.T) {
	legalStMutation(t, "SC", nil)
}
