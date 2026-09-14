package legal_test

// LEGAL-ST-FL-001: Florida's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from florida.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-fl/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_FL_001(t *testing.T) {
	legalStAssertPack(t, "FL", []legalStExpectation{
		// $14.00/hr on the constitutional glide path to $15.00 then CPI
		// (Fla. Const. Art. X § 24).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"14.00", "Art. X"}},
		// Written, legitimate-business-interest reasonableness; CHOICE Act
		// 4-year extension for covered high earners (Fla. Stat. § 542.335).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"542.335"}},
		// Sex-based pay-equity review (Fla. Stat. § 448.07).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"448.07"}},
		// E-Verify mandatory at 25+ employees (Fla. Stat. § 448.095, SB 1718).
		{Kind: legal.ObligationTypeEVerify, Requires: []string{"25"}},
		// Whistleblower protection conditioned on written internal notice
		// (Fla. Stat. § 448.101-105).
		{Kind: legal.ObligationTypeAntiRetaliation, Requires: []string{"448.101", "whistleblower"}},
		// 30-day breach notice, AG notice at 500+ residents
		// (Fla. Stat. § 501.171).
		{Kind: legal.ObligationTypeBreachNotification, Requires: []string{"501.171", `"subject_deadline_days":30`}},
	})
}

func TestTodo_LEGAL_ST_FL_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "FL", nil)
	legalStGoldens(t, "FL", p, release, registry)
}

func TestTodo_LEGAL_ST_FL_001_Conformance(t *testing.T) {
	legalStConformance(t, "FL", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_FL_001_Mutation(t *testing.T) {
	legalStMutation(t, "FL", nil)
}
