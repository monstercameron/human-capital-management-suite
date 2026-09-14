package legal_test

// LEGAL-ST-IL-001: Illinois's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from illinois.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-il/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_IL_001(t *testing.T) {
	legalStAssertPack(t, "IL", []legalStExpectation{
		// State minimum $15.00/hr with Chicago/Cook County overlays
		// (820 ILCS 105).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"15.00"}},
		// One full pay period of advance notice for pay/basis/payday/
		// deduction changes (820 ILCS 115/3).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"115/3", "BEFORE"}},
		// Pay-scale-in-posting and Equal Pay Registration Certificate
		// duties (820 ILCS 112).
		{Kind: legal.ObligationTypePayTransparency, Requires: []string{"820 ILCS 112", "internal_promotion"}},
		// Salary-history ban (the corpus binds the credit-history
		// restriction at 820 ILCS 70; the posting ban rides 820 ILCS 112).
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"820 ILCS"}},
		// Enhanced itemized-statement fields with retention
		// (820 ILCS 115/9 family).
		{Kind: legal.ObligationTypePayStatement, Requires: []string{"115/9"}},
		// Paid Leave for All Workers Act accrual preservation
		// (820 ILCS 192).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"820 ILCS 192"}},
		// Non-competes void below $75,000 / non-solicits below $45,000
		// (820 ILCS 90).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"820 ILCS 90"}},
		// Personnel-file inspection window (820 ILCS 40).
		{Kind: legal.ObligationTypePersonnelFile, Requires: []string{"820 ILCS 40"}},
		// BIPA written release before biometric collection
		// (740 ILCS 14).
		{Kind: legal.ObligationTypeMonitoringConsent, Requires: []string{"740 ILCS 14", "WRITTEN"}},
		// Illinois WARN mass-layoff notice (820 ILCS 65).
		{Kind: legal.ObligationTypeMiniWARN, Requires: []string{"820 ILCS 65"}},
	})
}

func TestTodo_LEGAL_ST_IL_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "IL", nil)
	legalStGoldens(t, "IL", p, release, registry)
}

func TestTodo_LEGAL_ST_IL_001_Conformance(t *testing.T) {
	legalStConformance(t, "IL", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_IL_001_Mutation(t *testing.T) {
	legalStMutation(t, "IL", nil)
}
