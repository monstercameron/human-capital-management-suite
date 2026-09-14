package legal_test

// LEGAL-ST-CO-001: Colorado's promotion/base-pay obligation parameters
// carried in a reviewed RulePack generated from colorado.md and published
// through the section 7.1 pipeline at the status it honestly earns; the
// section 8.1 artifacts live under testdata/legal/us-co/.

import (
	"testing"

	legal "github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
)

func TestTodo_LEGAL_ST_CO_001(t *testing.T) {
	legalStAssertPack(t, "CO", []legalStExpectation{
		// $15.16/hr statewide floor (CRS § 8-6-102 family).
		{Kind: legal.ObligationTypeWageFloor, Requires: []string{"15.16"}},
		// EPEWA pay-range/benefits/close-date posting and promotion notice
		// (CRS § 8-5-101 et seq.).
		{Kind: legal.ObligationTypePayTransparency, Requires: []string{"8-5-102", "internal_promotion"}},
		// Salary-history and pay-secrecy-policy bans (CRS § 8-5-103).
		{Kind: legal.ObligationTypeFieldRestriction, Requires: []string{"8-5-103", "salary_history"}},
		// At-least-monthly pay frequency (CRS § 8-4-102).
		{Kind: legal.ObligationTypePayFrequency, Requires: []string{"8-4-102"}},
		// Immediate on discharge / next payday on resignation, treble
		// damages for willful violation (CRS §§ 8-4-109, 8-4-105).
		{Kind: legal.ObligationTypeFinalPayDeadline, Requires: []string{"8-4-109"}},
		// HFWA 48-hour paid sick leave (CRS § 8-13.3-401 et seq.).
		{Kind: legal.ObligationTypeLeaveInteraction, Requires: []string{"8-13.3-401", "48 hours"}},
		// Non-compete void below the highly-compensated threshold and no
		// broader than trade-secret protection (CRS § 8-2-113).
		{Kind: legal.ObligationTypeNonCompete, Requires: []string{"8-2-113"}},
		// Once-per-calendar-year personnel-file inspection (CRS § 8-2-129).
		{Kind: legal.ObligationTypePersonnelFile, Requires: []string{"8-2-129", `"response_days":10`}},
		// Colorado AI Act duties on consequential employment decisions
		// (SB 24-205).
		{Kind: legal.ObligationTypeAutomatedDecision, Requires: []string{"24-205", "promotion"}},
	})
}

func TestTodo_LEGAL_ST_CO_001_Golden(t *testing.T) {
	p, release, registry, _ := legalStPack(t, "CO", nil)
	legalStGoldens(t, "CO", p, release, registry)
}

func TestTodo_LEGAL_ST_CO_001_Conformance(t *testing.T) {
	legalStConformance(t, "CO", nil, legal.ReviewStatusRequiresCustomerCounselConfiguration)
}

func TestTodo_LEGAL_ST_CO_001_Mutation(t *testing.T) {
	legalStMutation(t, "CO", nil)
}
