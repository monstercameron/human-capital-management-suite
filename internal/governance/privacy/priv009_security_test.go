package privacy

import (
	"errors"
	"strings"
	"testing"
)

// TestTodo_PRIV_009_Security is the SECURITY matrix test for PRIV-009: the
// matrix refuses incomplete or hostile inputs fail-closed, tampered
// certificates never validate, and decisions carry no subject identifiers.
func TestTodo_PRIV_009_Security(t *testing.T) {
	t.Run("an incomplete matrix never constructs", func(t *testing.T) {
		matrix := fixtureNotificationMatrix(t)
		short := matrix.Rules[:len(matrix.Rules)-1] // drop CONTACT_INFO/TENANT_CONTRACT
		if _, err := NewNotificationMatrix(fxBreachMatrixVersion, short); !errors.Is(err, ErrBreachBlocked) {
			t.Fatalf("NewNotificationMatrix(missing cell) = %v, want %v", err, ErrBreachBlocked)
		}
	})

	t.Run("a matrix with an undeclared duty or class never constructs", func(t *testing.T) {
		matrix := fixtureNotificationMatrix(t)
		rogue := append(matrix.Rules, MatrixRule{
			Duty: "REGULATOR_WHIM", Class: BreachPaymentCard,
			MinConsumers: 1, DeadlineHours: 24, Authority: "none",
		})
		if _, err := NewNotificationMatrix(fxBreachMatrixVersion, rogue); !errors.Is(err, ErrBreachBlocked) {
			t.Fatalf("NewNotificationMatrix(rogue duty) = %v, want %v", err, ErrBreachBlocked)
		}
	})

	t.Run("an incident naming an undeclared data class never decides", func(t *testing.T) {
		incident := fixtureBreachIncident(t)
		incident.AffectedClasses = append(incident.AffectedClasses, BreachDataClass("BIOMETRIC"))
		incident.EvidenceID = breachEvidencePrefix + "incident:" + incident.Digest()
		if _, err := DecideNotifications(incident, fixtureNotificationMatrix(t), mustInstant(t, fxBreachDecidedAt)); !errors.Is(err, ErrBreachBlocked) {
			t.Fatalf("DecideNotifications(undeclared class) = %v, want %v", err, ErrBreachBlocked)
		}
	})

	t.Run("evidence sealed before discovery never decides", func(t *testing.T) {
		incident := fixtureBreachIncident(t)
		incident.EvidenceSealedAt = mustInstant(t, fxBreachDiscoveredAt-1)
		incident.EvidenceID = breachEvidencePrefix + "incident:" + incident.Digest()
		if _, err := DecideNotifications(incident, fixtureNotificationMatrix(t), mustInstant(t, fxBreachDecidedAt)); !errors.Is(err, ErrBreachBlocked) {
			t.Fatalf("DecideNotifications(backdated seal) = %v, want %v", err, ErrBreachBlocked)
		}
	})

	t.Run("a decision made before the evidence seal never issues", func(t *testing.T) {
		incident := fixtureBreachIncident(t)
		if _, err := DecideNotifications(incident, fixtureNotificationMatrix(t), mustInstant(t, fxBreachSealedAt-1)); !errors.Is(err, ErrBreachBlocked) {
			t.Fatalf("DecideNotifications(pre-seal decision) = %v, want %v", err, ErrBreachBlocked)
		}
	})

	t.Run("a hand-edited certificate no longer validates", func(t *testing.T) {
		decision, err := DecideNotifications(fixtureBreachIncident(t), fixtureNotificationMatrix(t), mustInstant(t, fxBreachDecidedAt))
		if err != nil {
			t.Fatalf("DecideNotifications: %v", err)
		}
		decision.Decisions[0].Decision = DecisionNoNotify // attacker downgrades a NOTIFY
		if err := decision.Validate(); err == nil {
			t.Fatal("Validate(downgraded decision) succeeded, want a digest-mismatch failure")
		}
	})

	t.Run("a certificate rebound to another incident never validates", func(t *testing.T) {
		decision, err := DecideNotifications(fixtureBreachIncident(t), fixtureNotificationMatrix(t), mustInstant(t, fxBreachDecidedAt))
		if err != nil {
			t.Fatalf("DecideNotifications: %v", err)
		}
		decision.IncidentID = "breach-2026-002"
		if err := decision.Validate(); err == nil {
			t.Fatal("Validate(rebound incident) succeeded, want a digest-mismatch failure")
		}
	})

	t.Run("decisions carry duties, counts and grounds -- never subject identifiers", func(t *testing.T) {
		decision, err := DecideNotifications(fixtureBreachIncident(t), fixtureNotificationMatrix(t), mustInstant(t, fxBreachDecidedAt))
		if err != nil {
			t.Fatalf("DecideNotifications: %v", err)
		}
		for _, dec := range decision.Decisions {
			for _, field := range []string{dec.Authority, dec.Reason} {
				lower := strings.ToLower(field)
				if strings.Contains(lower, "@") || strings.Contains(lower, "ssn") || strings.Contains(lower, "card-number") {
					t.Errorf("duty %s evidence field %q carries subject-shaped data", dec.Duty, field)
				}
			}
		}
	})
}
