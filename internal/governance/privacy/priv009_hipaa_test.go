package privacy

import (
	"errors"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/governance/privacy/hipaa"
)

func rev09902HIPAAIncident(t *testing.T, applicability hipaa.Applicability, role hipaa.EntityRole, extensionVersion string) BreachIncident {
	t.Helper()
	incident := fixtureBreachIncident(t)
	incident.AffectedClasses = []BreachDataClass{BreachHealthInfo}
	incident.HIPAAApplicability = applicability
	incident.HIPAAEntityRole = role
	incident.HIPAAMatrixVersion = extensionVersion
	incident.EvidenceID = breachEvidencePrefix + "incident:" + incident.Digest()
	if err := incident.validate(); err != nil {
		t.Fatalf("HIPAA incident fixture: %v", err)
	}
	return incident
}

func rev09902HIPAAMatrix(t *testing.T) NotificationMatrix {
	t.Helper()
	extension := hipaa.StandardBreachMatrixExtension(fxBreachMatrixVersion)
	matrix, err := NewNotificationMatrixWithHIPAA(fxBreachMatrixVersion, fixtureNotificationMatrix(t).Rules, extension)
	if err != nil {
		t.Fatalf("NewNotificationMatrixWithHIPAA: %v", err)
	}
	return matrix
}

func hipaaClock(decision NotificationDecision, recipient hipaa.BreachRecipient, jurisdiction string) (HIPAAClockDecision, bool) {
	for _, clock := range decision.HIPAAClocks {
		if clock.Recipient == recipient && clock.Jurisdiction == jurisdiction {
			return clock, true
		}
	}
	return HIPAAClockDecision{}, false
}

func TestTodo_REV_099_02_PRIV009Primary(t *testing.T) {
	matrix := rev09902HIPAAMatrix(t)
	incident := rev09902HIPAAIncident(t, hipaa.ApplicabilityAssumed, hipaa.EntityRoleBusinessAssociate, hipaa.BreachMatrixVersion)
	decision, err := DecideNotifications(incident, matrix, mustInstant(t, fxBreachDecidedAt))
	if err != nil {
		t.Fatalf("HIPAA-aware DecideNotifications: %v", err)
	}
	if decision.HIPAAMatrixVersion != hipaa.BreachMatrixVersion || decision.HIPAAApplicability != hipaa.ApplicabilityAssumed {
		t.Fatalf("HIPAA matrix/coverage pin = %q/%q", decision.HIPAAMatrixVersion, decision.HIPAAApplicability)
	}
	if dutyAnswer(decision, DutyHIPAA, "") != DecisionNotify {
		t.Fatalf("HIPAA summary duty = %s, want NOTIFY", dutyAnswer(decision, DutyHIPAA, ""))
	}
	individual, ok := hipaaClock(decision, hipaa.RecipientIndividuals, "")
	if !ok || individual.Decision != DecisionNotify || individual.DeadlineDays != 60 || individual.DeadlineFrom != "discovery" {
		t.Fatalf("individual clock = %+v, present=%t", individual, ok)
	}
	mediaCA, ok := hipaaClock(decision, hipaa.RecipientMedia, "US-CA")
	if !ok || mediaCA.Decision != DecisionNotify || mediaCA.Consumers != fxBreachCACount {
		t.Fatalf("CA media clock = %+v, present=%t", mediaCA, ok)
	}
	mediaNY, ok := hipaaClock(decision, hipaa.RecipientMedia, "US-NY")
	if !ok || mediaNY.Decision != DecisionNoNotify || mediaNY.Consumers != fxBreachNYCount {
		t.Fatalf("NY media clock = %+v, present=%t", mediaNY, ok)
	}
	secretary, ok := hipaaClock(decision, hipaa.RecipientSecretary, "")
	if !ok || secretary.Decision != DecisionNotify || !strings.Contains(secretary.DeadlineFrom, "contemporaneous") {
		t.Fatalf("500+ Secretary clock = %+v, present=%t", secretary, ok)
	}
	coveredEntity, ok := hipaaClock(decision, hipaa.RecipientCoveredEntity, "")
	if !ok || coveredEntity.Decision != DecisionNotify || coveredEntity.DeadlineDays != 60 {
		t.Fatalf("business-associate-to-covered-entity clock = %+v, present=%t", coveredEntity, ok)
	}
	if err := decision.Validate(); err != nil {
		t.Fatalf("HIPAA decision certificate failed validation: %v", err)
	}

	small := rev09902HIPAAIncident(t, hipaa.ApplicabilityConfirmed, hipaa.EntityRoleCoveredEntity, hipaa.BreachMatrixVersion)
	small.Populations = []JurisdictionPopulation{{Jurisdiction: fxBreachNY(), Consumers: 499}}
	small.EvidenceID = breachEvidencePrefix + "incident:" + small.Digest()
	smallDecision, err := DecideNotifications(small, matrix, mustInstant(t, fxBreachDecidedAt))
	if err != nil {
		t.Fatalf("under-500 HIPAA decision: %v", err)
	}
	secretary, ok = hipaaClock(smallDecision, hipaa.RecipientSecretary, "")
	if !ok || !strings.Contains(secretary.DeadlineFrom, "calendar year") || !strings.Contains(secretary.Authority, "164.408(c)") {
		t.Fatalf("under-500 Secretary annual clock = %+v, present=%t", secretary, ok)
	}
	coveredEntity, ok = hipaaClock(smallDecision, hipaa.RecipientCoveredEntity, "")
	if !ok || coveredEntity.Decision != DecisionNoNotify {
		t.Fatalf("covered-entity role received a business-associate clock: %+v, present=%t", coveredEntity, ok)
	}
}

func TestTodo_REV_099_02_PRIV009Fault(t *testing.T) {
	incident := rev09902HIPAAIncident(t, hipaa.ApplicabilityConfirmed, hipaa.EntityRoleCoveredEntity, hipaa.BreachMatrixVersion)
	if _, err := DecideNotifications(incident, fixtureNotificationMatrix(t), mustInstant(t, fxBreachDecidedAt)); !errors.Is(err, ErrBreachBlocked) {
		t.Fatalf("health incident without HIPAA matrix = %v, want fail-closed block", err)
	}
	matrix := rev09902HIPAAMatrix(t)
	changedExtension := hipaa.StandardBreachMatrixExtension(fxBreachMatrixVersion)
	changedExtension.Rules[0].DeadlineDays = 61
	if _, err := NewNotificationMatrixWithHIPAA(fxBreachMatrixVersion, fixtureNotificationMatrix(t).Rules, changedExtension); !errors.Is(err, ErrBreachBlocked) {
		t.Fatalf("mutated HIPAA clock matrix constructed: %v", err)
	}
	incident.HIPAAMatrixVersion = "hipaa-breach-v999"
	incident.EvidenceID = breachEvidencePrefix + "incident:" + incident.Digest()
	if _, err := DecideNotifications(incident, matrix, mustInstant(t, fxBreachDecidedAt)); !errors.Is(err, ErrBreachBlocked) {
		t.Fatalf("incident bound to another HIPAA clock version = %v, want block", err)
	}
	incident = fixtureBreachIncident(t)
	incident.AffectedClasses = []BreachDataClass{BreachHealthInfo}
	incident.HIPAAApplicability = hipaa.ApplicabilityUnresolved
	incident.HIPAAEntityRole = hipaa.EntityRoleBusinessAssociate
	incident.HIPAAMatrixVersion = hipaa.BreachMatrixVersion
	incident.EvidenceID = breachEvidencePrefix + "incident:" + incident.Digest()
	if _, err := DecideNotifications(incident, matrix, mustInstant(t, fxBreachDecidedAt)); !errors.Is(err, ErrBreachBlocked) {
		t.Fatalf("unresolved HIPAA coverage = %v, want block", err)
	}
	validIncident := rev09902HIPAAIncident(t, hipaa.ApplicabilityAssumed, hipaa.EntityRoleBusinessAssociate, hipaa.BreachMatrixVersion)
	decision, err := DecideNotifications(validIncident, matrix, mustInstant(t, fxBreachDecidedAt))
	if err != nil {
		t.Fatalf("valid HIPAA decision: %v", err)
	}
	decision.HIPAAClocks[0].Decision = DecisionNoNotify
	if err := decision.Validate(); !errors.Is(err, ErrBreachBlocked) {
		t.Fatalf("tampered HIPAA clock certificate validated: %v", err)
	}
	notApplicable := rev09902HIPAAIncident(t, hipaa.ApplicabilityNotApplicable, "", "")
	decision, err = DecideNotifications(notApplicable, fixtureNotificationMatrix(t), mustInstant(t, fxBreachDecidedAt))
	if err != nil {
		t.Fatalf("explicitly out-of-scope health data should retain the existing matrix decision: %v", err)
	}
	if dutyAnswer(decision, DutyHIPAA, "") != "" || len(decision.HIPAAClocks) != 0 {
		t.Fatalf("not-applicable incident emitted HIPAA results: %+v", decision.HIPAAClocks)
	}
}
