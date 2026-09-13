package legal_test

// LEGAL-ST-DE-001: Delaware's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from delaware.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-de/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_DE_001(t *testing.T) {
	legalStAssertPack(t, "DE", []legalStExpectation{
		// $15.00/hr floor (19 Del. C. § 902).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"15.00", "902"}},
		// Salary-history ban (19 Del. C. § 709B).
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"709B", "salary_history"}},
		// Monthly minimum pay frequency (19 Del. C. § 1102).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"MONTHLY"}},
		// Next regular payday on layoff (19 Del. C. § 1103).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"next regular payday", "termination_any"}},
		// Delaware Paid Leave insurance program duties (19 Del. C. ch. 37).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"Paid Leave"}},
		// Once-yearly inspection, per-refusal penalty (19 Del. C. §§ 730-735).
		{Kind: legal.ObligationTypePersonnelFile, Requires: []string{"730"}},
		// 100+-employee / 50+-affected 60-day mass-layoff notice
		// (19 Del. C. ch. 19).
		{Kind: legal.ObligationTypeMiniWARN, Requires: []string{"60"}},
		// 60-day breach notice, AG notice at 500+ residents, SSN credit
		// monitoring (6 Del. C. § 12B-102).
		{Kind: legal.ObligationTypeBreachNotification, Requires: []string{"12B-102", `"subject_deadline_days":60`}},
	})
}

func TestTodo_LEGAL_ST_DE_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "DE", nil)
	legalStGoldens(t, "DE", p, release, registry)
}

func TestTodo_LEGAL_ST_DE_001_Conformance(t *testing.T) {
	legalStConformance(t, "DE", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_DE_001_Mutation(t *testing.T) {
	legalStMutation(t, "DE", nil)
}
