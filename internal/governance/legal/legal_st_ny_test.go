package legal_test

// LEGAL-ST-NY-001: New York's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from new-york.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-ny/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_NY_001(t *testing.T) {
	legalStAssertPack(t, "NY", []legalStExpectation{
		// Regional tiers: NYC/LI/Westchester $17.00/hr, rest $16.00/hr,
		// CPI-W adjusted (Labor Law § 652).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"17.00", "652"}},
		// Wage Theft Prevention Act hire notice plus 7-day pre-decrease
		// notice (Labor Law § 195).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"194-b"}},
		// Good-faith wage ranges in postings/transfers/promotions at 4+
		// employers (§ 194-b) and the salary-history ban (§ 194-a).
		{Kind: legal.ObligationTypePayTransparency, Requires: []string{"194-b", "internal_promotion"}},
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"194-a", "salary_history"}},
		// 40-hour paid sick leave plus Paid Family Leave and Paid
		// Prenatal Leave (§§ 196-b, 196-c).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"196-b"}},
		// NY WARN: 50+-employee threshold, 90-day notice
		// (Labor Law Art. 25-A).
		{Kind: legal.ObligationTypeMiniWARN, Requires: []string{`"notice_days":90`}},
		// Personnel-file access pending S.3460/A.2107 resolution — the
		// matrix cell is `?` so the obligation emits VERIFY-marked
		// (Labor Law § 210-b family).
		{Kind: legal.ObligationTypePersonnelFile, Requires: []string{"210-b"}},
	})
}

func TestTodo_LEGAL_ST_NY_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "NY", nil)
	legalStGoldens(t, "NY", p, release, registry)
}

func TestTodo_LEGAL_ST_NY_001_Conformance(t *testing.T) {
	legalStConformance(t, "NY", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_NY_001_Mutation(t *testing.T) {
	legalStMutation(t, "NY", nil)
}
