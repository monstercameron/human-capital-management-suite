package privacy

import (
	"errors"
	"testing"
)

// TestTodo_PRIV_009_Mutation is the MUTATION matrix test for PRIV-009. Each
// case moves one semantic input across a decision boundary and proves the
// certificate moves with it.
func TestTodo_PRIV_009_Mutation(t *testing.T) {
	t.Run("a consumer count at the threshold notifies, one below does not", func(t *testing.T) {
		matrix := fixtureNotificationMatrix(t)
		// FEDERAL/PAYMENT_CARD threshold is 1000; the fixture incident
		// affects only payment data here so that one rule decides it.
		atThreshold := fixtureBreachIncident(t)
		atThreshold.AffectedClasses = []BreachDataClass{BreachPaymentCard}
		atThreshold.Populations = []JurisdictionPopulation{{Jurisdiction: fxBreachCA(), Consumers: 1000}}
		atThreshold.EvidenceID = breachEvidencePrefix + "incident:" + atThreshold.Digest()
		decided, err := DecideNotifications(atThreshold, matrix, mustInstant(t, fxBreachDecidedAt))
		if err != nil {
			t.Fatalf("DecideNotifications: %v", err)
		}
		if got := dutyAnswer(decided, DutyFederal, ""); got != DecisionNotify {
			t.Errorf("federal answer at exactly the threshold = %s, want %s (inclusive boundary)", got, DecisionNotify)
		}

		below := fixtureBreachIncident(t)
		below.AffectedClasses = []BreachDataClass{BreachPaymentCard}
		below.Populations = []JurisdictionPopulation{{Jurisdiction: fxBreachCA(), Consumers: 999}}
		below.EvidenceID = breachEvidencePrefix + "incident:" + below.Digest()
		decided, err = DecideNotifications(below, matrix, mustInstant(t, fxBreachDecidedAt))
		if err != nil {
			t.Fatalf("DecideNotifications: %v", err)
		}
		if got := dutyAnswer(decided, DutyFederal, ""); got != DecisionNoNotify {
			t.Errorf("federal answer one below the threshold = %s, want %s", got, DecisionNoNotify)
		}
	})

	t.Run("state law judges state residents, never the national total", func(t *testing.T) {
		matrix := fixtureNotificationMatrix(t)
		// STATE/PAYMENT_CARD threshold is 500. CA's 400 residents alone
		// fall short even though the national total (400 + 1200 NY)
		// clears it; NY's 1200 clears it on its own.
		incident := fixtureBreachIncident(t)
		incident.AffectedClasses = []BreachDataClass{BreachPaymentCard}
		incident.Populations = []JurisdictionPopulation{
			{Jurisdiction: fxBreachCA(), Consumers: 400},
			{Jurisdiction: fxBreachNY(), Consumers: 1200},
		}
		incident.EvidenceID = breachEvidencePrefix + "incident:" + incident.Digest()
		decided, err := DecideNotifications(incident, matrix, mustInstant(t, fxBreachDecidedAt))
		if err != nil {
			t.Fatalf("DecideNotifications: %v", err)
		}
		if got := dutyAnswer(decided, DutyStateBreachLaw, "US-CA"); got != DecisionNoNotify {
			t.Errorf("CA state answer (400 residents) = %s, want %s", got, DecisionNoNotify)
		}
		if got := dutyAnswer(decided, DutyStateBreachLaw, "US-NY"); got != DecisionNotify {
			t.Errorf("NY state answer (1200 residents) = %s, want %s", got, DecisionNotify)
		}
	})

	t.Run("remediation at the seal instant is allowed, one second before is not", func(t *testing.T) {
		sealed := fixtureBreachIncident(t)
		sealed.RemediatedAt = mustInstant(t, fxBreachSealedAt)
		sealed.EvidenceID = breachEvidencePrefix + "incident:" + sealed.Digest()
		if _, err := DecideNotifications(sealed, fixtureNotificationMatrix(t), mustInstant(t, fxBreachDecidedAt)); err != nil {
			t.Errorf("DecideNotifications(remediation at seal): %v, want success (the boundary is inclusive)", err)
		}

		early := fixtureBreachIncident(t)
		early.RemediatedAt = mustInstant(t, fxBreachSealedAt-1)
		early.EvidenceID = breachEvidencePrefix + "incident:" + early.Digest()
		if _, err := DecideNotifications(early, fixtureNotificationMatrix(t), mustInstant(t, fxBreachDecidedAt)); !errors.Is(err, ErrBreachBlocked) {
			t.Errorf("DecideNotifications(remediation before seal) = %v, want %v", err, ErrBreachBlocked)
		}
	})

	t.Run("a rule with no positive threshold or deadline never constructs", func(t *testing.T) {
		matrix := fixtureNotificationMatrix(t)
		lowered := append([]MatrixRule(nil), matrix.Rules...)
		lowered[0].MinConsumers = 0
		if _, err := NewNotificationMatrix(fxBreachMatrixVersion, lowered); !errors.Is(err, ErrBreachBlocked) {
			t.Errorf("NewNotificationMatrix(zero threshold) = %v, want %v", err, ErrBreachBlocked)
		}
		lowered = append([]MatrixRule(nil), matrix.Rules...)
		lowered[0].DeadlineHours = 0
		if _, err := NewNotificationMatrix(fxBreachMatrixVersion, lowered); !errors.Is(err, ErrBreachBlocked) {
			t.Errorf("NewNotificationMatrix(zero deadline) = %v, want %v", err, ErrBreachBlocked)
		}
	})

	t.Run("duplicate matrix cells never construct", func(t *testing.T) {
		matrix := fixtureNotificationMatrix(t)
		doubled := append(append([]MatrixRule(nil), matrix.Rules...), matrix.Rules[0])
		if _, err := NewNotificationMatrix(fxBreachMatrixVersion, doubled); !errors.Is(err, ErrBreachBlocked) {
			t.Errorf("NewNotificationMatrix(duplicate cell) = %v, want %v", err, ErrBreachBlocked)
		}
	})

	t.Run("a held incident's decision names its hold", func(t *testing.T) {
		incident := fixtureBreachIncident(t) // HoldRef litigation-2026-04
		decided, err := DecideNotifications(incident, fixtureNotificationMatrix(t), mustInstant(t, fxBreachDecidedAt))
		if err != nil {
			t.Fatalf("DecideNotifications: %v", err)
		}
		if decided.HoldRef != "litigation-2026-04" {
			t.Errorf("decision HoldRef = %q, want the incident's hold carried into the certificate", decided.HoldRef)
		}
		unheld := fixtureBreachIncident(t)
		unheld.HoldRef = ""
		unheld.EvidenceID = breachEvidencePrefix + "incident:" + unheld.Digest()
		decided, err = DecideNotifications(unheld, fixtureNotificationMatrix(t), mustInstant(t, fxBreachDecidedAt))
		if err != nil {
			t.Fatalf("DecideNotifications (unheld): %v", err)
		}
		if decided.HoldRef != "" {
			t.Errorf("unheld decision HoldRef = %q, want empty", decided.HoldRef)
		}
		if decided.Digest() == "" {
			t.Error("unheld decision has no digest")
		}
	})
}

// dutyAnswer returns the recorded answer for duty (and jurisdiction for
// state law), or "" when the duty was never decided.
func dutyAnswer(d NotificationDecision, duty Duty, jurisdiction string) NotifyDecision {
	for _, dec := range d.Decisions {
		if dec.Duty == duty && dec.Jurisdiction == jurisdiction {
			return dec.Decision
		}
	}
	return ""
}
