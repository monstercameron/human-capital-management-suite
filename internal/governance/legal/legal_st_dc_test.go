package legal_test

// LEGAL-ST-DC-001: the District of Columbia's promotion/base-pay obligation
// parameters researched into district-of-columbia.md, carried in a reviewed
// RulePack generated from it, and published through the section 7.1 pipeline
// at the status it honestly earns; the section 8.1 artifacts live under
// testdata/legal/us-dc/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_DC_001(t *testing.T) {
	legalStAssertPack(t, "DC", []legalStExpectation{
		// $17.95/hr (2025-07-01) rising to $18.40/hr (2026-07-01),
		// CPI-indexed, all employer sizes (D.C. Code § 32-1003).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"17.95", "32-1003"}},
		// WTPAA Notice of Hire at hire plus the 30-day updated-notice
		// duty on any change to the notice's contents
		// (§ 32-1008).
		{Kind: legal.ObligationTypeNotice, Requires: []string{"32-1008"}},
		// Good-faith min/max salary in all listings including promotion
		// and transfer opportunities (§ 32-1453.01).
		{Kind: legal.ObligationTypePayTransparency, Requires: []string{"32-1453"}},
		// Wage-history screening ban (§ 32-1452).
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"32-145", "salary_history"}},
		// ASSLA size-tiered accrual plus the employer-funded Universal
		// Paid Leave benefit (§ 32-531.02, § 32-541.01 et seq.).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{}},
		// Non-competes void below the minimum qualifying annual
		// compensation — $150,000/$250,000 medical specialists,
		// CPI-adjusted (§ 32-581.01 et seq., applicable 2022-10-01).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{}},
		// Discharge pays the next working day; resignation the earlier
		// of next payday or 7 days; 10%-per-working-day liquidated
		// damages capped at treble (§ 32-1303).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"32-1303"}},
		// Paydays at least twice monthly (§ 32-1302 family).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{}},
		// Equal-pay duties under the Wage Transparency Act / DCHRA.
		{Kind: legal.ObligationTypePayEquityReview, Requires: []string{}},
		// Payroll records and signed notices retained 3 years.
		{Kind: legal.ObligationTypeRetention, Requires: []string{}},
		// No private-sector personnel-file access statute exists: the
		// matrix cell is F.
		{Kind: legal.ObligationTypePersonnelFile, Absent: true},
	})
}

func TestTodo_LEGAL_ST_DC_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "DC", nil)
	legalStGoldens(t, "DC", p, release, registry)
}

func TestTodo_LEGAL_ST_DC_001_Conformance(t *testing.T) {
	legalStConformance(t, "DC", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}
