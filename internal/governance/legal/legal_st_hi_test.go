package legal_test

// LEGAL-ST-HI-001: Hawaii's promotion/base-pay obligation parameters carried
// in a reviewed RulePack generated from hawaii.md and published through the
// section 7.1 pipeline at the status it honestly earns; the section 8.1
// artifacts live under testdata/legal/us-hi/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_HI_001(t *testing.T) {
	legalStAssertPack(t, "HI", []legalStExpectation{
		// $16.00/hr (2026) scheduled to $18.00/hr (2028) (HRS § 387-2).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"16.00", "387-2"}},
		// Salary-history ban for all employers (HRS § 378-2.4).
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"378-2.4", "salary_history"}},
		// Wage/salary-range posting for 50+ employers (HRS § 378-2.3, Act
		// 203).
		{Kind: legal.ObligationTypePayTransparency, Requires: []string{"378-2.3", "internal_promotion"}},
		// Semi-monthly pay frequency (HRS § 388-2).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"388-2", "SEMIMONTHLY"}},
		// At discharge or next working day when immediate payment is
		// impossible (HRS § 388-3).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"388"}},
		// HFLL unpaid-leave protection plus TDI wage replacement
		// (HRS § 378-71a, ch. 392).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"378-71a"}},
		// Technology-worker exemption and post-employment-covenant limits
		// (Act 158; Prudential Locations v. Gagnon).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"non-compete"}},
		// 50+-employee 60-day mini-WARN notice plus severance (HRS § 394B).
		{Kind: legal.ObligationTypeMiniWARN, Requires: []string{"394B", `"employee_threshold":50`}},
	})
}

func TestTodo_LEGAL_ST_HI_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "HI", nil)
	legalStGoldens(t, "HI", p, release, registry)
}

func TestTodo_LEGAL_ST_HI_001_Conformance(t *testing.T) {
	legalStConformance(t, "HI", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_HI_001_Mutation(t *testing.T) {
	legalStMutation(t, "HI", nil)
}
