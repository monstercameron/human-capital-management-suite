package application

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/advisory"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/incidentstate"
)

// TestTodo_REV_017_03_Integration is the REV-017-03 integration test for the
// advisory half: it drives a real Declared-to-Mitigating transition through
// the application layer's incident-transition entry point (the package the
// served binaries compose) and asserts OPS-005's advisory publication fires
// with the incident's scoped affected set, while a same-status transition
// stays silent.
func TestTodo_REV_017_03_Integration(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	audience := advisory.Audience{TenantID: "tenant-a", Role: "tenant-customer", Authorized: true}
	delivery := advisory.DeliveryEvidence{ReceiptID: "rcpt-1", Channel: "status-page", Status: "delivered", At: now}
	before := incidentstate.Incident{
		ID: "inc-1", TenantID: "tenant-a", State: incidentstate.Declared, Version: 3,
		Owner: "commander", Mitigation: "fix rolling out", RepairLink: "repair-1",
		MonitoringUntil: now.Add(-time.Hour),
		Affected: incidentstate.AffectedSet{Known: true, Verified: true, Facts: []incidentstate.AffectedFact{
			{TenantID: "tenant-a", Kind: "capability", Value: "promotion"},
			{TenantID: "tenant-a", Kind: "worker", Value: "worker-secret"},
		}},
	}

	pub := advisory.NewPublisher()
	after, published, didPublish, err := ApplyIncidentTransition(pub, before,
		incidentstate.Command{To: incidentstate.Mitigating, Actor: "commander", Reason: "fix rolling out", EvidenceRef: "mit-1"},
		now, audience, delivery)
	if err != nil {
		t.Fatalf("ApplyIncidentTransition: %v", err)
	}
	if after.State != incidentstate.Mitigating || after.Version != 4 {
		t.Fatalf("transitioned incident = %+v, want MITIGATING v4", after)
	}
	if !didPublish || published.Status != advisory.StatusMitigating {
		t.Fatalf("published = %+v, %v, want the MITIGATING advisory", published, didPublish)
	}
	if len(published.Facts) != 1 || published.Facts[0].Value != "promotion" {
		t.Fatalf("published facts = %+v, want only the safe-kind fact", published.Facts)
	}
	if history := pub.History("tenant-a", "inc-1"); len(history) != 1 || history[0].Digest != published.Digest {
		t.Fatalf("publisher history = %+v, want the published advisory", history)
	}

	monitoring, _, monDid, err := ApplyIncidentTransition(pub, after,
		incidentstate.Command{To: incidentstate.Monitoring, Actor: "commander", Reason: "watching", EvidenceRef: "mon-1", MonitoringUntil: now.Add(time.Hour)},
		now, audience, delivery)
	if err != nil {
		t.Fatalf("monitoring ApplyIncidentTransition: %v", err)
	}
	if monitoring.State != incidentstate.Monitoring || !monDid {
		t.Fatalf("monitoring transition = %+v, %v, want MONITORING with an advisory", monitoring, monDid)
	}
	// The monitoring window (13:00) elapses before the resolve step (14:00),
	// so the closure prerequisites hold and the silent same-status step
	// reaches RESOLVED.
	resolveAt := now.Add(2 * time.Hour)
	silent, silentPub, silentDid, err := ApplyIncidentTransition(pub, monitoring,
		incidentstate.Command{To: incidentstate.Resolved, Actor: "commander", Reason: "contained", EvidenceRef: "res-1"},
		resolveAt, audience, delivery)
	if err != nil {
		t.Fatalf("resolved ApplyIncidentTransition: %v", err)
	}
	if silent.State != incidentstate.Resolved {
		t.Fatalf("transitioned incident = %+v, want RESOLVED", silent)
	}
	if silentDid {
		t.Fatalf("MONITORING to RESOLVED published %+v, want silence (both map to customer MONITORING)", silentPub)
	}
}
