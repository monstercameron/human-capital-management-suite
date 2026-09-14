package legal_test

// LEGAL-ST-OH-001: Ohio's promotion/base-pay obligation parameters carried
// in a reviewed RulePack generated from ohio.md and published through the
// section 7.1 pipeline at the status it honestly earns; the section 8.1
// artifacts live under testdata/legal/us-oh/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_OH_001(t *testing.T) {
	legalStAssertPack(t, "OH", []legalStExpectation{
		// $11.00/hr (2026) above the $405,000 gross-receipts threshold,
		// CPI-indexed (Ohio Const. Art. II § 34a).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"11.00", "Art. II"}},
		// Semimonthly paydays with the mid-month cadence
		// (R.C. 4113.15).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"SEMIMONTHLY"}},
		// Itemized wage-statement mandate (R.C. 4113.14 / HB 106).
		{Kind: legal.ObligationTypePayStatement, Requires: []string{"pay_rate"}},
		// Earned sick/safe time for all private employers (Issue 1).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"accrued paid sick leave"}},
		// Mini-WARN: 100+-employee/50+-affected-site 60-day notice
		// (R.C. 4113.31, eff. 2025-09-29).
		{Kind: legal.ObligationTypeMiniWARN, Requires: []string{`"notice_days":60`}},
		// Broad-basis pay equity, all employers (R.C. 4111.17).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"equal work"}},
	})
}

func TestTodo_LEGAL_ST_OH_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "OH", nil)
	legalStGoldens(t, "OH", p, release, registry)
}

func TestTodo_LEGAL_ST_OH_001_Conformance(t *testing.T) {
	legalStConformance(t, "OH", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_OH_001_Mutation(t *testing.T) {
	legalStMutation(t, "OH", nil)
}
