package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// rev035Clock returns a fixed clock and the instant it reports.
func rev035Clock() (func() time.Time, time.Time) {
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return now }, now
}

func rev035Sweeper(t *testing.T, registry *schedule.Registry, now time.Time) *TriggerSweeper {
	t.Helper()
	clock := func() time.Time { return now }
	dispatcher, err := schedule.NewDispatcher(schedule.DispatchPolicy{MaxAge: time.Hour, Clock: clock})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	quarantine, err := schedule.NewQuarantineStore(schedule.QuarantinePolicy{MaxAttempts: 3, Clock: clock})
	if err != nil {
		t.Fatalf("NewQuarantineStore: %v", err)
	}
	sweeper, err := NewTriggerSweeper(TriggerSweepConfig{
		Registry:    registry,
		Dispatcher:  dispatcher,
		Quarantine:  quarantine,
		Lookback:    time.Hour,
		Misfire:     schedule.MisfireConfig{Policy: schedule.MisfireSkip, Grace: time.Hour},
		Zone:        values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"},
		LeaderID:    "test-replica",
		TargetScope: []string{"org:acme"},
	})
	if err != nil {
		t.Fatalf("NewTriggerSweeper: %v", err)
	}
	return sweeper
}

// TestTodo_REV_035_01 proves the composed trigger sweep dispatches due
// occurrences: a published, activated cron trigger produces created
// dispatch outcomes naming intents instead of sitting uncalled in the
// schedule package.
func TestTodo_REV_035_01(t *testing.T) {
	_, now := rev035Clock()
	registry := schedule.NewRegistry()
	publishRev035Trigger(t, registry, "tenant-1", "sweep-cron", cronRev035Source("* * * * *"))
	sweeper := rev035Sweeper(t, registry, now)
	report, err := sweeper.Sweep(context.Background(), now)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if report.Dispatched == 0 {
		t.Fatalf("Sweep dispatched nothing: %+v", report)
	}
	if len(report.IntentIDs) != report.Dispatched {
		t.Fatalf("Sweep dispatched %d outcomes but named %d intents: %+v", report.Dispatched, len(report.IntentIDs), report)
	}
	if report.Quarantined != 0 {
		t.Fatalf("healthy sweep quarantined %d: %+v", report.Quarantined, report)
	}
}

// TestTodo_REV_035_01_Integration drives the sweep the way the scheduler
// workload composes it: a healthy cron trigger dispatches to named intents
// while an event-source trigger becomes a quarantined dead letter, and a
// second sweep replays duplicates instead of naming the intents twice.
func TestTodo_REV_035_01_Integration(t *testing.T) {
	_, now := rev035Clock()
	registry := schedule.NewRegistry()
	publishRev035Trigger(t, registry, "tenant-1", "sweep-cron", cronRev035Source("* * * * *"))
	publishRev035Trigger(t, registry, "tenant-1", "sweep-event", eventRev035Source())
	sweeper := rev035Sweeper(t, registry, now)
	first, err := sweeper.Sweep(context.Background(), now)
	if err != nil {
		t.Fatalf("first Sweep: %v", err)
	}
	if first.Dispatched == 0 || len(first.IntentIDs) == 0 {
		t.Fatalf("first sweep produced no intents: %+v", first)
	}
	if first.Quarantined != 1 {
		t.Fatalf("first sweep quarantined %d, want the event trigger: %+v", first.Quarantined, first)
	}
	if len(first.Letters) != 1 {
		t.Fatalf("first sweep letters = %d, want 1 dead letter: %+v", len(first.Letters), first)
	}
	second, err := sweeper.Sweep(context.Background(), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("second Sweep: %v", err)
	}
	if second.Dispatched != 1 {
		t.Fatalf("second sweep dispatched %d, want exactly the one new occurrence: %+v", second.Dispatched, second)
	}
	seen := make(map[string]bool, len(first.IntentIDs))
	for _, id := range first.IntentIDs {
		seen[id] = true
	}
	for _, id := range second.IntentIDs {
		if seen[id] {
			t.Fatalf("second sweep renamed already-dispatched intent %s", id)
		}
	}
}

func cronRev035Source(expression string) schedule.TriggerSource {
	return schedule.TriggerSource{Kind: schedule.SourceCron, Cron: schedule.CronSource{Expression: expression}}
}

func eventRev035Source() schedule.TriggerSource {
	return schedule.TriggerSource{
		Kind:  schedule.SourceEvent,
		Event: schedule.EventFilter{EventType: schedule.EventDeadlineDue, SchemaVersion: "1"},
	}
}

// publishRev035Trigger publishes and activates one trigger revision in the
// sweep registry, the same Publish/Activate path production uses.
func publishRev035Trigger(t *testing.T, registry *schedule.Registry, tenant, id string, source schedule.TriggerSource) schedule.PublishedTrigger {
	t.Helper()
	target := intent.Ref{TypeID: "hcmnext.schedule.tick", Version: 1}
	def := schedule.TriggerDefinition{
		ID:                   id,
		Version:              "1",
		TenantID:             tenant,
		Target:               target,
		InputTemplateDigest:  "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Purpose:              "workforce.review",
		Owner:                "workforce-operations",
		Source:               source,
		Overlap:              schedule.OverlapQueue,
		Storm:                schedule.StormPolicy{MaxFiringsPerWindow: 20, Window: time.Hour, JitterBound: 5 * time.Minute},
		ExecutionMode:        intent.ModeExecute,
		ExecutionEnvironment: intent.EnvironmentProduction,
	}
	targets := []schedule.AuthorizedTarget{{
		Ref:          target,
		Purposes:     []string{def.Purpose},
		AllowedModes: []intent.Mode{intent.ModeExecute},
		PublisherRef: intent.Ref{TypeID: "hcmnext.schedule.publisher", Version: 1},
	}}
	published, err := schedule.Publish(registry, def, targets)
	if err != nil {
		t.Fatalf("Publish %s: %v", id, err)
	}
	if _, err := registry.Activate(published.Ref(), schedule.ActivationEvidence{
		ActivatedBy: "test",
		Authority:   "test",
		Reason:      "sweep proof",
		ActivatedAt: time.Date(2026, 9, 16, 11, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Activate %s: %v", id, err)
	}
	return published
}
