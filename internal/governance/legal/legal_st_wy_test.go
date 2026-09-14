package legal_test

// LEGAL-ST-WY-001: Wyoming's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from wyoming.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-wy/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_WY_001(t *testing.T) {
	legalStAssertPack(t, "WY", []legalStExpectation{
		// Federal-only floor (Wyo. Stat. § 27-4-202): no state-floor
		// obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// Semimonthly for industrial/factory operations, itemized stub
		// (§ 27-4-101).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"27-4-101"}},
		// Next regular payday; 18% interest plus attorney fees if
		// wrongfully withheld (§ 27-4-104).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"27-4-104"}},
		// Non-competes void except executives/managerial staff, business
		// sale, trade secrets, expense recovery — eff. 2025-07-01
		// (SF 107).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"SF 107"}},
		// "Comparable work" pay equity within the establishment
		// (§ 27-4-302).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"27-4-302"}},
		// HANDBOOK_DISCLAIMER standard preserving at-will status
		// (Trabing v. Kinko's).
		{Kind: legal.ObligationTypeJobSecurity, Requires: []string{"HANDBOOK_DISCLAIMER"}},
		// The drug-testing obligation's missing statutory citation is
		// carried VERIFY-marked as the corpus extraction finding
		// records.
		{Kind: legal.ObligationTypeDrugTesting, Requires: []string{}},
	})
}

func TestTodo_LEGAL_ST_WY_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "WY", nil)
	legalStGoldens(t, "WY", p, release, registry)
}

func TestTodo_LEGAL_ST_WY_001_Conformance(t *testing.T) {
	legalStConformance(t, "WY", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_WY_001_Mutation(t *testing.T) {
	legalStMutation(t, "WY", nil)
}
