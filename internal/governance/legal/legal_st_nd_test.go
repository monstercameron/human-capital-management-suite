package legal_test

// LEGAL-ST-ND-001: North Dakota's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from north-dakota.md and
// published through the section 7.1 pipeline at the status it honestly
// earns; the section 8.1 artifacts live under testdata/legal/us-nd/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_ND_001(t *testing.T) {
	legalStAssertPack(t, "ND", []legalStExpectation{
		// Federal-only floor (no state increase since 2009): no
		// state-floor obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// Regular, predictable paydays (NDCC § 34-14-02).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"34-14-02"}},
		// Earlier of next regular payday or 15 days with the narrow
		// accrued-leave-payout exceptions (NDCC § 34-14-03).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"34-14-03"}},
		// Non-competes void except business-sale/partnership-dissolution —
		// the corpus's strictest voidance rule (NDCC § 9-08-06).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"9-08-06"}},
		// Comparable-work pay equity (NDCC § 34-06.1).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"34-06.1"}},
		// Lawful off-duty activity protection (NDCC § 14-02.4 family;
		// corpus binds the whistleblower section § 34-01-20).
		{Kind: legal.ObligationTypeAntiRetaliation, Requires: []string{"34-01-20"}},
	})
}

func TestTodo_LEGAL_ST_ND_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "ND", nil)
	legalStGoldens(t, "ND", p, release, registry)
}

func TestTodo_LEGAL_ST_ND_001_Conformance(t *testing.T) {
	legalStConformance(t, "ND", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_ND_001_Mutation(t *testing.T) {
	legalStMutation(t, "ND", nil)
}
