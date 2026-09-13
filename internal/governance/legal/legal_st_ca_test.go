package legal_test

// LEGAL-ST-CA-001: California's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from california.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-ca/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_CA_001(t *testing.T) {
	legalStAssertPack(t, "CA", []legalStExpectation{
		// General tier $16.90/hr, CPI-W-indexed (Lab. Code § 1182.12).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"16.90", "1182.12"}},
		// Wage-theft-prevention hire notice and 7-calendar-day change notice
		// (Lab. Code § 2810.5).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"2810.5", `"timing_days":7`}},
		// Salary-history ban (Lab. Code § 432.3).
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"432.3", "salary_history"}},
		// Pay-scale disclosure on request and in postings (Lab. Code § 432.3).
		{Kind: legal.ObligationTypePayTransparency, Requires: []string{"432.3", "internal_promotion"}},
		// 3-year wage/title retention tied to the itemized-statement duty
		// (Lab. Code § 226).
		{Kind: legal.ObligationTypeRetention, Requires: []string{"226", `"duration_years":3`}},
		// Semimonthly pay frequency (Lab. Code § 204).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"204", "SEMIMONTHLY"}},
		// Nine-field itemized wage statement (Lab. Code § 226).
		{Kind: legal.ObligationTypePayStatement, Requires: []string{"226", "pay_rate"}},
		// Paid-sick-leave interaction carried on the final-pay section the
		// corpus binds the item to (Lab. Code §§ 201-203 / § 246 family).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"paid"}},
		// Immediate on discharge, 72-hour on unnoticed resignation
		// (Lab. Code §§ 201-203).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"201"}},
		// Non-competes void except business sale (B&P § 16600).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"16600"}},
		// ABC classification test (AB 5 / Lab. Code § 2750.5).
		{Kind: legal.ObligationTypeClassification, Requires: []string{"ABC"}},
		// Equal Pay Act review plus pay-data reporting (Lab. Code § 1197.5,
		// Gov. Code § 12999).
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{"1197.5"}},
		// 30-day (extendable 35) personnel-file inspection (§ 1198.5).
		{Kind: legal.ObligationTypePersonnelFile, Requires: []string{"1198.5", `"response_days":30`}},
		// Cal-WARN: 60-day notice, 50+-employee threshold (§§ 1400-1408).
		{Kind: legal.ObligationTypeMiniWARN, Requires: []string{"1400", `"notice_days":60`}},
		// CCPA/CPRA employee-data coverage, notice-only consent posture.
		{Kind: legal.ObligationTypeMonitoringConsent, Requires: []string{"1798.100", "NOTICE_ONLY"}},
	})
}

func TestTodo_LEGAL_ST_CA_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "CA", nil)
	legalStGoldens(t, "CA", p, release, registry)
}

func TestTodo_LEGAL_ST_CA_001_Conformance(t *testing.T) {
	legalStConformance(t, "CA", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_CA_001_Mutation(t *testing.T) {
	legalStMutation(t, "CA", nil)
}
