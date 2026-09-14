package legal_test

// LEGAL-ST-VT-001: Vermont's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from vermont.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-vt/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_VT_001(t *testing.T) {
	legalStAssertPack(t, "VT", []legalStExpectation{
		// $14.42/hr (2026), lower-of-5%-or-CPI index, tipped $7.21/hr
		// (21 V.S.A. § 384).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"14.42", "384"}},
		// Range disclosure for 5+ employers, eff. 2025-07-01
		// (Act 155, § 495n).
		{Kind: legal.ObligationTypePayTransparency, Requires: []string{"495n"}},
		// Salary-history ban, no compensation bounds as hiring
		// conditions (§ 495m).
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"495m", "salary_history"}},
		// Within 72 hours on discharge (§ 342a).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"342a"}},
		// Earned Sick Time plus unpaid parental/family leave; the
		// voluntary PFML insurance program is RECOMMENDED, never a
		// mandatory contribution (§§ 481-486, Act 32).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{}},
		// Ban-the-box until after interview qualification (§ 495j
		// family).
		{Kind: legal.ObligationTypeAntiRetaliation, Requires: []string{"495"}},
		// 45-day breach notice, 14-day AG notice (9 V.S.A. § 2435).
		{Kind: legal.ObligationTypeBreachNotification, Requires: []string{`"subject_deadline_days":45`}},
	})
}

func TestTodo_LEGAL_ST_VT_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "VT", nil)
	legalStGoldens(t, "VT", p, release, registry)
}

func TestTodo_LEGAL_ST_VT_001_Conformance(t *testing.T) {
	legalStConformance(t, "VT", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_VT_001_Mutation(t *testing.T) {
	legalStMutation(t, "VT", nil)
}
