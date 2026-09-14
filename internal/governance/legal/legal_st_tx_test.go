package legal_test

// LEGAL-ST-TX-001: Texas's promotion/base-pay obligation parameters carried
// in a reviewed RulePack generated from texas.md and published through the
// section 7.1 pipeline at the status it honestly earns; the section 8.1
// artifacts live under testdata/legal/us-tx/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_TX_001(t *testing.T) {
	legalStAssertPack(t, "TX", []legalStExpectation{
		// Federal-only floor (§ 62.151): no state-floor obligation.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// Semi-monthly for non-exempt, monthly for exempt
		// (§§ 61.011, 61.012).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"61.011"}},
		// Within 6 calendar days on discharge, next regular payday on
		// resignation (§ 61.014).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"61.014"}},
		// Ancillary-to-enforceable-agreement requirement; healthcare
		// professions capped 1-year/5-mile with buyout cap eff.
		// 2025-09-01 (§ 15.50, SB 1318).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"15.50"}},
		// 60 days to individuals, 30 days to the AG at 250+ residents
		// (§ 521.053 family).
		{Kind: legal.ObligationTypeBreachNotification, Requires: []string{`"subject_deadline_days":60`}},
		// Workers'-compensation retaliation, 2-year limitation
		// (§ 451.001).
		{Kind: legal.ObligationTypeAntiRetaliation, Requires: []string{"451.001"}},
		// LEAVE_INTERACTION and locality overlays preempted statewide
		// (HB 2127): PreemptionAssertion, not an obligation.
		{Kind: legal.ObligationTypeLeaveInteraction, Absent: true},
	})
}

func TestTodo_LEGAL_ST_TX_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "TX", nil)
	legalStGoldens(t, "TX", p, release, registry)
}

func TestTodo_LEGAL_ST_TX_001_Conformance(t *testing.T) {
	legalStConformance(t, "TX", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_TX_001_Mutation(t *testing.T) {
	legalStMutation(t, "TX", nil)
}
