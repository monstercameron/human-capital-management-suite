package legal_test

// LEGAL-ST-NH-001: New Hampshire's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from new-hampshire.md and
// published through the section 7.1 pipeline at the status it honestly
// earns; the section 8.1 artifacts live under testdata/legal/us-nh/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_NH_001(t *testing.T) {
	legalStAssertPack(t, "NH", []legalStExpectation{
		// Federal-only floor (RSA 279:21 at $7.25): no state-floor
		// obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// Written advance notice of any wage/salary/payday change with
		// signed acknowledgment (RSA 275:49).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"written", "BEFORE"}},
		// Weekly or biweekly default frequency with Commissioner-approved
		// exceptions (RSA 275:43).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{}},
		// Non-competes void at or below 200% of federal minimum wage
		// (RSA 275:70-a, 329:31-a).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"275:70"}},
		// Sex-based pay equity plus wage-discussion protection
		// (RSA 275:37, 275:41-b).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"equal work"}},
		// Statutory personnel-file inspection right, 1-year retention at
		// 15+ employees (RSA 275:56).
		{Kind: legal.ObligationTypePersonnelFile, Requires: []string{}},
		{Kind: legal.ObligationTypeRetention, Requires: []string{`"duration_years":1`}},
	})
}

func TestTodo_LEGAL_ST_NH_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "NH", nil)
	legalStGoldens(t, "NH", p, release, registry)
}

func TestTodo_LEGAL_ST_NH_001_Conformance(t *testing.T) {
	legalStConformance(t, "NH", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_NH_001_Mutation(t *testing.T) {
	legalStMutation(t, "NH", nil)
}
