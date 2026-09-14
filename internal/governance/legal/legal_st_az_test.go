package legal_test

// LEGAL-ST-AZ-001: Arizona's promotion/base-pay obligation parameters carried
// in a reviewed RulePack generated from arizona.md and published through the
// section 7.1 pipeline at the status it honestly earns; the section 8.1
// artifacts live under testdata/legal/us-az/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_AZ_001(t *testing.T) {
	legalStAssertPack(t, "AZ", []legalStExpectation{
		// Statewide floor under Prop. 206 (A.R.S. § 23-363); the CPI-indexed
		// figure is carried as a blocking gap while the corpus records no
		// numeric floor in the section the extractor read.
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"23-363"}},
		// Two-plus paydays per month, ≤16 days apart (A.R.S. § 23-351).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"23-351", "MONTHLY"}},
		// Discharge = earlier of 7 working days or next regular payday
		// (A.R.S. § 23-353).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"23-353", "7"}},
		// Paid sick time under A.R.S. § 23-371 et seq.
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"23-37"}},
		// E-Verify mandatory for all employers (A.R.S. § 23-214).
		{Kind: legal.ObligationTypeEVerify, Requires: []string{"23-214"}},
		// Payroll-record retention duty (A.R.S. § 23-364).
		{Kind: legal.ObligationTypeRetention, Requires: []string{"23-364"}},
		// Sex-based pay equity (A.R.S. § 23-341).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"23-341"}},
	})
}

func TestTodo_LEGAL_ST_AZ_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "AZ", nil)
	legalStGoldens(t, "AZ", p, release, registry)
}

func TestTodo_LEGAL_ST_AZ_001_Conformance(t *testing.T) {
	legalStConformance(t, "AZ", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_AZ_001_Mutation(t *testing.T) {
	legalStMutation(t, "AZ", nil)
}
