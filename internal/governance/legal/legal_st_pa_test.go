package legal_test

// LEGAL-ST-PA-001: Pennsylvania's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from pennsylvania.md and
// published through the section 7.1 pipeline at the status it honestly
// earns; the section 8.1 artifacts live under testdata/legal/us-pa/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_PA_001(t *testing.T) {
	legalStAssertPack(t, "PA", []legalStExpectation{
		// Federal-only floor (43 P.S. § 333.101 et seq. at $7.25): no
		// state-floor obligation emitted.
		{Kind: legal.ObligationTypeWageFloor, Absent: true},
		// Written notice at hire and in advance of wage/frequency/
		// deduction/benefit changes (43 P.S. § 260.4).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"260", "BEFORE"}},
		// Next regular payday regardless of separation reason, 25%
		// liquidated damages (WPCL, 43 P.S. § 260 et seq.).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"260.3"}},
		// Sex-only equal-pay statute (43 P.S. § 336.1).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"336.1"}},
		// Common-law reasonableness; healthcare-practitioner covenants
		// capped at 1 year (Act 74, eff. 2025-01-01).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"common law"}},
		// Human Relations Act coverage at 4+ employees (43 P.S. § 951
		// et seq.; corpus binds the jury-duty protection).
		{Kind: legal.ObligationTypeAntiRetaliation, Requires: []string{"jury_duty"}},
	})
}

func TestTodo_LEGAL_ST_PA_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "PA", nil)
	legalStGoldens(t, "PA", p, release, registry)
}

func TestTodo_LEGAL_ST_PA_001_Conformance(t *testing.T) {
	legalStConformance(t, "PA", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_PA_001_Mutation(t *testing.T) {
	legalStMutation(t, "PA", nil)
}
