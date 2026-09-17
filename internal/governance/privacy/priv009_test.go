package privacy

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// TestTodo_PRIV_009 is the PRIMARY test for planning/todos.md PRIV-009:
// "Produce a reproducible financial-breach and notification-decision matrix."
//
// RED (todos.md PRIV-009): "an incident touching payment, bank-detail or FTI
// data has no recorded discovery time, affected-data scope, jurisdiction
// matrix or regulator/customer notice decision, so a federal, state, GLBA
// or tenant-contract notice duty cannot be shown to have been evaluated."
//
// GREEN (todos.md PRIV-009): "an incident workflow resolves affected
// data/population, discovery instant, jurisdiction-matrix version,
// consumer-count calculation and a reproducible notify/no-notify decision
// per duty (federal, state breach law, GLBA, tenant contract), preserving
// evidence before any remediation that would alter it."
func TestTodo_PRIV_009(t *testing.T) {
	t.Run("GREEN: a sealed payment-and-FTI incident decides every duty reproducibly", func(t *testing.T) {
		incident := fixtureBreachIncident(t)
		matrix := fixtureNotificationMatrix(t)
		first, err := DecideNotifications(incident, matrix, mustInstant(t, fxBreachDecidedAt))
		if err != nil {
			t.Fatalf("DecideNotifications: %v", err)
		}
		if err := first.Validate(); err != nil {
			t.Fatalf("DecideNotifications produced an invalid decision: %v", err)
		}
		duties := coveredDuties(first)
		for _, duty := range []Duty{DutyFederal, DutyStateBreachLaw, DutyGLBA, DutyTenantContract} {
			if !duties[duty] {
				t.Errorf("duty %s has no decision: federal, state, GLBA and tenant-contract duties must all be evaluated", duty)
			}
		}
		if first.ConsumerCount != fxBreachCACount+fxBreachNYCount {
			t.Errorf("ConsumerCount = %d, want the calculated %d", first.ConsumerCount, fxBreachCACount+fxBreachNYCount)
		}
		if first.EvidenceID == "" || first.EvidenceID != breachEvidencePrefix+first.Digest() {
			t.Errorf("EvidenceID = %q, want it to match the decision's own digest", first.EvidenceID)
		}
		second, err := DecideNotifications(incident, matrix, mustInstant(t, fxBreachDecidedAt))
		if err != nil {
			t.Fatalf("DecideNotifications (replay): %v", err)
		}
		if first.Digest() != second.Digest() {
			t.Error("the same incident and matrix decide differently on replay: the decision is not reproducible")
		}
	})

	t.Run("GREEN: a below-threshold incident records no-notify decisions with reasons, not silence", func(t *testing.T) {
		incident := fixtureBreachIncident(t)
		incident.Populations = []JurisdictionPopulation{{Jurisdiction: fxBreachCA(), Consumers: 1}}
		incident.EvidenceID = breachEvidencePrefix + "incident:" + incident.Digest()
		matrix := fixtureNotificationMatrix(t)
		decision, err := DecideNotifications(incident, matrix, mustInstant(t, fxBreachDecidedAt))
		if err != nil {
			t.Fatalf("DecideNotifications: %v", err)
		}
		found := false
		for _, d := range decision.Decisions {
			if d.Decision == DecisionNoNotify {
				found = true
				if d.Reason == "" {
					t.Errorf("duty %s no-notify decision carries no reason", d.Duty)
				}
			}
		}
		if !found {
			t.Error("a below-threshold incident produced no NO_NOTIFY decision at all")
		}
	})

	t.Run("RED: an incident with no discovery time, scope, population or matrix version never decides", func(t *testing.T) {
		matrix := fixtureNotificationMatrix(t)
		base := fixtureBreachIncident(t)
		cases := map[string]func(*BreachIncident){
			"no discovery time": func(i *BreachIncident) { i.DiscoveredAt = values.Instant{} },
			"no data scope":     func(i *BreachIncident) { i.AffectedClasses = nil },
			"no population":     func(i *BreachIncident) { i.Populations = nil },
			"no matrix version": func(i *BreachIncident) { i.MatrixVersion = "" },
			"unsealed evidence": func(i *BreachIncident) { i.EvidenceSealedAt = values.Instant{} },
		}
		for name, mutate := range cases {
			incident := base
			mutate(&incident)
			if _, err := DecideNotifications(incident, matrix, mustInstant(t, fxBreachDecidedAt)); !errors.Is(err, ErrBreachBlocked) {
				t.Errorf("%s: DecideNotifications = %v, want %v", name, err, ErrBreachBlocked)
			}
		}
	})

	t.Run("RED: remediation before evidence preservation blocks the decision", func(t *testing.T) {
		incident := fixtureBreachIncident(t)
		incident.RemediatedAt = mustInstant(t, fxBreachSealedAt-1) // remediated one second before the seal
		if _, err := DecideNotifications(incident, fixtureNotificationMatrix(t), mustInstant(t, fxBreachDecidedAt)); !errors.Is(err, ErrBreachBlocked) {
			t.Errorf("DecideNotifications(remediation before seal) = %v, want %v", err, ErrBreachBlocked)
		}
	})

	t.Run("RED: an incident evaluated against another matrix version never decides", func(t *testing.T) {
		incident := fixtureBreachIncident(t)
		incident.MatrixVersion = "breach-matrix-v999"
		if _, err := DecideNotifications(incident, fixtureNotificationMatrix(t), mustInstant(t, fxBreachDecidedAt)); !errors.Is(err, ErrBreachBlocked) {
			t.Errorf("DecideNotifications(matrix drift) = %v, want %v", err, ErrBreachBlocked)
		}
	})
}
