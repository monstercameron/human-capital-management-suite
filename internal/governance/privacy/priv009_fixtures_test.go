package privacy

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// --- PRIV-009 shared fixtures ------------------------------------------------

const (
	fxBreachTenant        = values.TenantId("tenant-acme")
	fxBreachDiscoveredAt  = 1_800_300_000
	fxBreachSealedAt      = 1_800_303_600
	fxBreachDecidedAt     = 1_800_307_200
	fxBreachCACount       = int64(1200)
	fxBreachNYCount       = int64(300)
	fxBreachMatrixVersion = "breach-matrix-v3"
)

func fxBreachCA() legal.Jurisdiction { return legal.Jurisdiction{Country: "US", State: "CA"} }
func fxBreachNY() legal.Jurisdiction { return legal.Jurisdiction{Country: "US", State: "NY"} }

// fixtureBreachIncident returns a sealed payment/bank-detail/FTI incident
// across two states, carrying a legal-hold reference: the GREEN workflow
// every PRIV-009 matrix test decides first.
func fixtureBreachIncident(t *testing.T) BreachIncident {
	t.Helper()
	incident := BreachIncident{
		ID:              "breach-2026-001",
		Tenant:          fxBreachTenant,
		DiscoveredAt:    mustInstant(t, fxBreachDiscoveredAt),
		AffectedClasses: []BreachDataClass{BreachPaymentCard, BreachBankDetail, BreachFTI},
		Populations: []JurisdictionPopulation{
			{Jurisdiction: fxBreachCA(), Consumers: fxBreachCACount},
			{Jurisdiction: fxBreachNY(), Consumers: fxBreachNYCount},
		},
		MatrixVersion:    fxBreachMatrixVersion,
		EvidenceSealedAt: mustInstant(t, fxBreachSealedAt),
		HoldRef:          "litigation-2026-04",
	}
	incident.EvidenceID = breachEvidencePrefix + "incident:" + incident.Digest()
	if err := incident.validate(); err != nil {
		t.Fatalf("fixture incident does not validate: %v", err)
	}
	return incident
}

// fixtureNotificationMatrix returns the complete v3 duty table every
// PRIV-009 matrix test decides against: all four duties by all six data
// classes, with thresholds the fixture incident's counts cross except
// where a test deliberately goes below them.
func fixtureNotificationMatrix(t *testing.T) NotificationMatrix {
	t.Helper()
	rule := func(duty Duty, class BreachDataClass, threshold, deadline int64, authority string) MatrixRule {
		return MatrixRule{Duty: duty, Class: class, MinConsumers: threshold, DeadlineHours: deadline, Authority: authority}
	}
	const fed, glba, state, contract = "FEDERAL-BREACH-STATUTE", "15-U.S.C.-6801-GLBA", "STATE-BREACH-STATUTE", "MSA-2026-SCHED-PRIVACY"
	rules := []MatrixRule{
		rule(DutyFederal, BreachPaymentCard, 1000, 72, fed),
		rule(DutyFederal, BreachBankDetail, 1000, 72, fed),
		rule(DutyFederal, BreachFTI, 1, 24, fed),
		rule(DutyFederal, BreachCredentials, 500, 72, fed),
		rule(DutyFederal, BreachHealthInfo, 500, 72, fed),
		rule(DutyFederal, BreachContactInfo, 5000, 72, fed),
		rule(DutyStateBreachLaw, BreachPaymentCard, 500, 720, state),
		rule(DutyStateBreachLaw, BreachBankDetail, 500, 720, state),
		rule(DutyStateBreachLaw, BreachFTI, 250, 720, state),
		rule(DutyStateBreachLaw, BreachCredentials, 500, 720, state),
		rule(DutyStateBreachLaw, BreachHealthInfo, 500, 720, state),
		rule(DutyStateBreachLaw, BreachContactInfo, 5000, 720, state),
		rule(DutyGLBA, BreachPaymentCard, 1, 72, glba),
		rule(DutyGLBA, BreachBankDetail, 1, 72, glba),
		rule(DutyGLBA, BreachFTI, 1, 72, glba),
		rule(DutyGLBA, BreachCredentials, 1000, 72, glba),
		rule(DutyGLBA, BreachHealthInfo, 100000, 72, glba),
		rule(DutyGLBA, BreachContactInfo, 100000, 72, glba),
		rule(DutyTenantContract, BreachPaymentCard, 1, 24, contract),
		rule(DutyTenantContract, BreachBankDetail, 1, 24, contract),
		rule(DutyTenantContract, BreachFTI, 1, 24, contract),
		rule(DutyTenantContract, BreachCredentials, 1, 24, contract),
		rule(DutyTenantContract, BreachHealthInfo, 1, 24, contract),
		rule(DutyTenantContract, BreachContactInfo, 1, 24, contract),
	}
	matrix, err := NewNotificationMatrix(fxBreachMatrixVersion, rules)
	if err != nil {
		t.Fatalf("NewNotificationMatrix (fixture): %v", err)
	}
	return matrix
}

// coveredDuties reports which duties carry at least one decision.
func coveredDuties(d NotificationDecision) map[Duty]bool {
	out := make(map[Duty]bool, len(d.Decisions))
	for _, dec := range d.Decisions {
		out[dec.Duty] = true
	}
	return out
}
