package legal_test

// LEGAL-ST-KS-001: Kansas's promotion/base-pay obligation parameters carried
// in a reviewed RulePack generated from kansas.md and published through the
// section 7.1 pipeline at the status it honestly earns; the section 8.1
// artifacts live under testdata/legal/us-ks/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_KS_001(t *testing.T) {
	legalStAssertPack(t, "KS", []legalStExpectation{
		// Federal-only floor (K.S.A. 44-1202 at $7.25): no state-floor
		// obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// Written pay-rate-change notice; reduction notice must precede
		// the affected work (K.S.A. 44-320, K.A.R. 49-20-1).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"written", "pay_rate"}},
		// At-least-semimonthly pay frequency (K.S.A. 44-313 to 44-327).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"SEMIMONTHLY"}},
		// Common-law reasonableness for non-competes; SB 241 non-solicit
		// presumptions and mandatory reformation.
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"SB 241"}},
		// Contractor classification (K.S.A. § 44-701).
		{Kind: legal.ObligationTypeClassification, Requires: []string{"44-701"}},
		// Breach notification without unreasonable delay
		// (K.S.A. 50-7a01 family).
		{Kind: legal.ObligationTypeBreachNotification, Requires: []string{`"subject_deadline_days":5`}},
	})
}

func TestTodo_LEGAL_ST_KS_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "KS", nil)
	legalStGoldens(t, "KS", p, release, registry)
}

func TestTodo_LEGAL_ST_KS_001_Conformance(t *testing.T) {
	legalStConformance(t, "KS", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_KS_001_Mutation(t *testing.T) {
	legalStMutation(t, "KS", nil)
}
