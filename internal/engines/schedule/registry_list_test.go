package schedule

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/intent"
)

// listActiveFixture publishes one cron trigger, mirroring the
// schedule_test.go definition fixture.
func listActiveFixture(t *testing.T, registry *Registry, id, version string) PublishedTrigger {
	t.Helper()
	def := TriggerDefinition{
		ID:                   id,
		Version:              version,
		TenantID:             "tenant-1",
		Target:               intent.Ref{TypeID: "hcmnext.schedule.tick", Version: 1},
		InputTemplateDigest:  "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Purpose:              "workforce.review",
		Owner:                "workforce-operations",
		Source:               TriggerSource{Kind: SourceCron, Cron: CronSource{Expression: "* * * * *"}},
		Overlap:              OverlapQueue,
		Storm:                StormPolicy{MaxFiringsPerWindow: 20, Window: time.Hour, JitterBound: 5 * time.Minute},
		ExecutionMode:        intent.ModeExecute,
		ExecutionEnvironment: intent.EnvironmentProduction,
	}
	targets := []AuthorizedTarget{{
		Ref:          def.Target,
		Purposes:     []string{def.Purpose},
		AllowedModes: []intent.Mode{intent.ModeExecute},
		PublisherRef: intent.Ref{TypeID: "hcmnext.schedule.publisher", Version: 1},
	}}
	published, err := Publish(registry, def, targets)
	if err != nil {
		t.Fatalf("Publish %s/%s: %v", id, version, err)
	}
	return published
}

// TestTodo_REV_035_01_ListActive proves the sweep enumeration seam lists
// exactly the activated revisions: a published-but-never-activated trigger
// is not due and must not be returned.
func TestTodo_REV_035_01_ListActive(t *testing.T) {
	registry := NewRegistry()
	active := listActiveFixture(t, registry, "sweep-cron", "1")
	listActiveFixture(t, registry, "sweep-idle", "1")
	if _, err := registry.Activate(active.Ref(), ActivationEvidence{
		ActivatedBy: "test",
		Authority:   "test",
		Reason:      "sweep enumeration proof",
		ActivatedAt: time.Date(2026, 9, 16, 11, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	listed := registry.ListActive()
	if len(listed) != 1 {
		t.Fatalf("ListActive returned %d triggers, want exactly the activated one", len(listed))
	}
	if listed[0].Ref() != active.Ref() {
		t.Fatalf("ListActive returned %s, want %s", listed[0].Ref(), active.Ref())
	}
	if string(listed[0].Canonical()) != string(active.Canonical()) {
		t.Fatal("ListActive did not return the published bytes")
	}
}
