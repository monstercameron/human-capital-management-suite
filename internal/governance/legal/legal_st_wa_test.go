package legal_test

// LEGAL-ST-WA-001: Washington's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from washington.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-wa/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_WA_001(t *testing.T) {
	legalStAssertPack(t, "WA", []legalStExpectation{
		// $17.13/hr (2026), CPI-W-indexed (RCW 49.46; corpus binds the
		// pay-range section on this item).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"49.58.110"}},
		// Salary-history ban plus pay-range posting at 15+ employers
		// with cure window and statutory damages (RCW 49.58).
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"49.58.100", "salary_history"}},
		{Kind: legal.ObligationTypePayTransparency, Requires: []string{"49.58.110", "internal_promotion"}},
		// All earned wages by end of the pay period, double damages for
		// willful non-payment (RCW 49.48.010).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"49.48.010"}},
		// Paid sick leave 1hr/40hrs plus PFML (RCW 49.46.210, 50A).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"49.46.210"}},
		// Non-competes unenforceable below the indexed salary threshold,
		// 18-month presumption (RCW 49.62.020).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"49.62.020"}},
		// 21-day personnel-file access window (RCW 49.12.240/250).
		{Kind: legal.ObligationTypePersonnelFile, Requires: []string{"49.12.240", `"response_days":21`}},
		// State WARN: 60-day notice, 50+ employees (SB 5525).
		{Kind: legal.ObligationTypeMiniWARN, Requires: []string{"SB 5525"}},
	})
}

func TestTodo_LEGAL_ST_WA_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "WA", nil)
	legalStGoldens(t, "WA", p, release, registry)
}

func TestTodo_LEGAL_ST_WA_001_Conformance(t *testing.T) {
	legalStConformance(t, "WA", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_WA_001_Mutation(t *testing.T) {
	legalStMutation(t, "WA", nil)
}
