package legal_test

// LEGAL-ST-IA-001: Iowa's promotion/base-pay obligation parameters carried
// in a reviewed RulePack generated from iowa.md and published through the
// section 7.1 pipeline at the status it honestly earns; the section 8.1
// artifacts live under testdata/legal/us-ia/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_IA_001(t *testing.T) {
	legalStAssertPack(t, "IA", []legalStExpectation{
		// Federal-only floor (Iowa Code § 91D.1 at $7.25): no state-floor
		// obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// 3-day advance written pay-rate-change notice (Iowa Code § 91A.3).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"91A.3", `"timing_days":3`, "BEFORE"}},
		// Next regular payday or within 5 days, whichever first
		// (Iowa Code § 91A.4).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"91A.4"}},
		// 3-business-day personnel-file access (Iowa Code § 91B.1).
		{Kind: legal.ObligationTypePersonnelFile, Requires: []string{"91B.1"}},
		// Iowa mini-WARN: 30-day notice for 25+-employee layoffs
		// (Iowa Code § 84C).
		{Kind: legal.ObligationTypeMiniWARN, Requires: []string{"84C"}},
	})
}

func TestTodo_LEGAL_ST_IA_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "IA", nil)
	legalStGoldens(t, "IA", p, release, registry)
}

func TestTodo_LEGAL_ST_IA_001_Conformance(t *testing.T) {
	legalStConformance(t, "IA", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_IA_001_Mutation(t *testing.T) {
	legalStMutation(t, "IA", nil)
}
