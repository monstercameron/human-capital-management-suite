package scheduled

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type ownerMemory struct {
	state  Schedule
	audits []Audit
}

func (m *ownerMemory) Load(_ context.Context, tenant, id string) (Schedule, bool, error) {
	return m.state, m.state.Trigger.Definition.TenantID == tenant && m.state.Trigger.Definition.ID == id, nil
}
func (m *ownerMemory) Save(_ context.Context, s Schedule, expected uint64, a Audit) error {
	if m.state.Revision != expected {
		return ErrRevision
	}
	m.state = s
	m.audits = append(m.audits, a)
	return nil
}

type ownerAuthority struct{ revoked bool }

func (a *ownerAuthority) Authorize(_ context.Context, actor Actor, action Action, s Schedule) ([]schedule.AuthorizedTarget, error) {
	if a.revoked || actor.TenantID != s.Trigger.Definition.TenantID || actor.UserID == "outsider" {
		return nil, ErrAuthority
	}
	return []schedule.AuthorizedTarget{{AgentRun: s.Trigger.Definition.AgentRun}}, nil
}
func (a *ownerAuthority) CheckCurrent(context.Context, Schedule) error {
	if a.revoked {
		return ErrAuthority
	}
	return nil
}
func ownerFixture(t *testing.T) (*Owner, *ownerMemory, *ownerAuthority, Schedule) {
	t.Helper()
	target := testFiring().Target
	def := schedule.TriggerDefinition{ID: "schedule-3", Version: "1", TenantID: "tenant-7", TargetKind: schedule.TargetAgentRun, AgentRun: &target, InputTemplateDigest: testContextDigest, Purpose: target.Purpose, Owner: "owner", Source: schedule.TriggerSource{Kind: schedule.SourceCron, Cron: schedule.CronSource{Expression: "0 9 * * *"}}, Overlap: schedule.OverlapSkip, Storm: schedule.StormPolicy{MaxFiringsPerWindow: 10, Window: 24 * time.Hour}, ExecutionMode: intent.ModeExecute, ExecutionEnvironment: intent.EnvironmentProduction}
	pub, err := schedule.Publish(schedule.NewRegistry(), def, []schedule.AuthorizedTarget{{AgentRun: &target}})
	if err != nil {
		t.Fatal(err)
	}
	s := Schedule{Trigger: pub, OwnerID: "owner", InstallationID: "installation", AgentPrincipalID: "agent-principal", LegalEntity: "entity", State: StateDraft, Zone: values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"}, Misfire: schedule.MisfireConfig{Policy: schedule.MisfireSkip}, RunTimeout: time.Hour, DST: "BOTH", Context: agentrun.ContextScope{ID: "context", SnapshotID: "snapshot", Digest: testContextDigest}}
	store := &ownerMemory{}
	authority := &ownerAuthority{}
	owner, err := NewOwner(store, authority)
	if err != nil {
		t.Fatal(err)
	}
	return owner, store, authority, s
}
func TestTodo_AGENT_030(t *testing.T) {
	ctx := context.Background()
	owner, store, _, s := ownerFixture(t)
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	draft, err := owner.Draft(ctx, Actor{"tenant-7", "owner"}, s, 0, at)
	if err != nil || draft.Revision != 1 {
		t.Fatalf("draft=%+v err=%v", draft, err)
	}
	active, err := owner.Control(ctx, Actor{"tenant-7", "reviewer"}, "schedule-3", 1, ActionPublish, "", at)
	if err != nil || active.State != StateActive || active.Trigger.Definition.AgentRun.Agent.Version != "3" {
		t.Fatalf("publication=%+v err=%v", active, err)
	}
	paused, err := owner.Control(ctx, Actor{"tenant-7", "owner"}, "schedule-3", 2, ActionPause, "", at)
	if err != nil || paused.State != StatePaused {
		t.Fatalf("pause=%+v err=%v", paused, err)
	}
	if _, err := owner.Control(ctx, Actor{"tenant-7", "owner"}, "schedule-3", 3, ActionDryRun, "", at); !errors.Is(err, ErrInactive) {
		t.Fatalf("paused dry run=%v", err)
	}
	resumed, err := owner.Control(ctx, Actor{"tenant-7", "owner"}, "schedule-3", 3, ActionResume, "", at)
	if err != nil || resumed.Revision != 4 || len(store.audits) != 4 {
		t.Fatalf("resume=%+v err=%v audits=%d", resumed, err, len(store.audits))
	}
	if _, err := owner.Control(ctx, Actor{"tenant-7", "owner"}, "schedule-3", 3, ActionPause, "", at); !errors.Is(err, ErrRevision) {
		t.Fatalf("stale control=%v", err)
	}
}
func TestTodo_AGENT_030_Security(t *testing.T) {
	owner, store, authority, s := ownerFixture(t)
	ctx := context.Background()
	at := time.Now().UTC()
	if _, err := owner.Draft(ctx, Actor{"tenant-7", "outsider"}, s, 0, at); !errors.Is(err, ErrAuthority) || len(store.audits) != 0 {
		t.Fatalf("unauthorized draft=%v audits=%d", err, len(store.audits))
	}
	if _, err := owner.Draft(ctx, Actor{"tenant-7", "owner"}, s, 0, at); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Control(ctx, Actor{"tenant-7", "owner"}, "schedule-3", 1, ActionPublish, "", at); !errors.Is(err, ErrAuthority) {
		t.Fatalf("self review=%v", err)
	}
	authority.revoked = true
	if _, err := owner.Control(ctx, Actor{"tenant-7", "reviewer"}, "schedule-3", 1, ActionPublish, "", at); !errors.Is(err, ErrAuthority) || store.state.Revision != 1 {
		t.Fatalf("revoked publication=%v state=%+v", err, store.state)
	}
}
func TestTodo_AGENT_030_Property(t *testing.T) {
	owner, _, _, s := ownerFixture(t)
	ctx := context.Background()
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for days := 1; days <= 7; days++ {
		window := schedule.OccurrenceWindow{Start: values.NewInstant(start), End: values.NewInstant(start.Add(time.Duration(days) * 24 * time.Hour))}
		first, err := owner.Preview(ctx, Actor{"tenant-7", "owner"}, s, window)
		if err != nil {
			t.Fatal(err)
		}
		second, err := owner.Preview(ctx, Actor{"tenant-7", "owner"}, s, window)
		if err != nil || first.Digest != second.Digest || len(first.Occurrences) != days {
			t.Fatalf("days=%d first=%+v second=%+v err=%v", days, first, second, err)
		}
		seen := map[string]bool{}
		for _, occ := range first.Occurrences {
			if seen[occ.Key] {
				t.Fatal("duplicate occurrence")
			}
			seen[occ.Key] = true
		}
	}
}

func TestTodo_AGENT_030_Golden(t *testing.T) {
	owner, _, _, s := ownerFixture(t)
	s.DST = "EARLIER"
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	preview, err := owner.Preview(context.Background(), Actor{"tenant-7", "owner"}, s, schedule.OccurrenceWindow{Start: values.NewInstant(at), End: values.NewInstant(at.Add(24 * time.Hour))})
	if err != nil {
		t.Fatal(err)
	}
	const expectedDigest = "sha256:2c6dd13a213b83e022ca35a42a71f055089fddf7cbc01be060854ba16bc754cd"
	if preview.Digest != expectedDigest {
		t.Fatalf("preview digest=%s", preview.Digest)
	}
	if len(preview.Occurrences) != 1 || preview.Occurrences[0].At.Time() != time.Date(2026, 11, 1, 14, 0, 0, 0, time.UTC) {
		t.Fatalf("preview=%+v", preview)
	}
}
