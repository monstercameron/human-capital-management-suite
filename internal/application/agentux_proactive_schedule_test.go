package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type proactiveScheduleStore struct {
	record scheduled.Schedule
	found  bool
}

func (s *proactiveScheduleStore) Load(context.Context, string, string) (scheduled.Schedule, bool, error) {
	return s.record, s.found, nil
}
func (s *proactiveScheduleStore) Save(_ context.Context, record scheduled.Schedule, expected uint64, _ scheduled.Audit) error {
	if s.found && s.record.Revision != expected {
		return scheduled.ErrRevision
	}
	s.record, s.found = record, true
	return nil
}

type proactiveScheduleBinding struct{ record scheduled.Schedule }

func (b proactiveScheduleBinding) ResolveAnnouncementScheduleBinding(context.Context, AgentAnnouncementActor, agentstore.Announcement) (scheduled.Schedule, error) {
	return b.record, nil
}

func TestAgentUXProactive_NativeSchedulePauseAndMisfire(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	target := schedule.AgentRunTarget{Agent: schedule.AgentVersionRef{ID: "policy-helper", Version: "1", Digest: digest}, SponsorID: "service", Purpose: "announcement", Budget: schedule.AgentRunBudget{MaxCostMicros: 100, MaxInputTokens: 200, MaxOutputTokens: 100}, Destination: schedule.AgentRunDestination{AudienceID: "general", AudienceSnapshotID: "audience", AudienceDigest: digest}}
	def := schedule.TriggerDefinition{ID: "base", Version: "1", TenantID: "tenant", TargetKind: schedule.TargetAgentRun, AgentRun: &target, InputTemplateDigest: digest, Purpose: "announcement", Owner: "owner", Source: schedule.TriggerSource{Kind: schedule.SourceCron, Cron: schedule.CronSource{Expression: "0 9 * * 1"}}, Overlap: schedule.OverlapSkip, Storm: schedule.StormPolicy{MaxFiringsPerWindow: 400, Window: 366 * 24 * time.Hour}, ExecutionMode: intent.ModeExecute, ExecutionEnvironment: intent.EnvironmentProduction}
	trigger, err := schedule.Publish(schedule.NewRegistry(), def, []schedule.AuthorizedTarget{{AgentRun: &target}})
	if err != nil {
		t.Fatal(err)
	}
	base := scheduled.Schedule{Trigger: trigger, State: scheduled.StateActive, OwnerID: "owner", InstallationID: "install", AgentPrincipalID: "agent-service", LegalEntity: "company", Zone: values.ZoneRef{ID: "America/New_York", TzdbVersion: "2026a"}, RunTimeout: time.Minute, Context: agentrun.ContextScope{ID: "announcement", SnapshotID: "revision", Digest: digest}, Misfire: schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, MaxCatchUp: 1}, DST: "EARLIER"}
	store := &proactiveScheduleStore{}
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	native := AgentAnnouncementNativeSchedules{Store: store, Bindings: proactiveScheduleBinding{record: base}, Now: func() time.Time { return at }}
	actor := AgentAnnouncementActor{TenantID: "tenant", TenantUUID: uuid.New(), SubjectID: "owner"}
	record := agentstore.Announcement{ID: "holidays", SchedulerID: "announcement:holidays", InstallationID: "install", PersonaID: "policy-helper", ConversationID: "general", Instruction: "Tell employees which holidays are coming up.", OwnerID: "owner", Zone: "America/New_York", Revision: 1, State: agentstore.AnnouncementActive}
	id, next, err := native.CreateAnnouncementSchedule(context.Background(), actor, record, "0 9 * * 1")
	if err != nil || id != record.SchedulerID || next == nil || next.Weekday() != time.Monday || next.Hour() != 13 {
		t.Fatalf("native creation: %s %v %v", id, next, err)
	}
	if store.record.Context.ID != record.ID || store.record.Context.Digest == digest || store.record.Misfire.Policy != schedule.MisfireCatchUpOnce || store.record.Misfire.MaxCatchUp != 1 {
		t.Fatalf("definition did not bind documents or one catch-up: %+v", store.record)
	}
	resumeAt := at.Add(30 * 24 * time.Hour)
	missed, err := schedule.CalculateOccurrences(store.record.Trigger, schedule.OccurrenceRequest{Window: schedule.OccurrenceWindow{Start: store.record.Cursor, End: values.NewInstant(resumeAt)}, Zone: store.record.Zone, Misfire: store.record.Misfire, ResumeAt: values.NewInstant(resumeAt)})
	if err != nil || len(missed.Occurrences) != 1 || missed.Occurrences[0].Misfire != schedule.MisfireCatch || !missed.Occurrences[0].At.Time().Before(resumeAt) || missed.Occurrences[0].At.Time().Day() != 26 {
		t.Fatalf("missed month did not collapse to one catch-up: %+v %v", missed, err)
	}
	if err := native.PauseAnnouncementSchedule(context.Background(), actor, id, 1); err != nil || store.record.State != scheduled.StatePaused {
		t.Fatalf("pause: %+v %v", store.record, err)
	}
	if next, err := native.ResumeAnnouncementSchedule(context.Background(), actor, id, 2); err != nil || next == nil || store.record.State != scheduled.StateActive {
		t.Fatalf("resume: %v %v", next, err)
	}
	at = time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	if next, err := native.NextAnnouncementRun(context.Background(), actor, record); err != nil || next == nil || next.Day() != 12 {
		t.Fatalf("next run retained the occurrence already posted: %v %v", next, err)
	}
	if err := native.DeleteAnnouncementSchedule(context.Background(), actor, id, 3); err != nil || store.record.State != scheduled.StateRetired {
		t.Fatalf("delete: %v", err)
	}
	actor.SubjectID = "other"
	if _, err := native.ResumeAnnouncementSchedule(context.Background(), actor, id, 4); !errors.Is(err, ErrAgentAnnouncementDenied) {
		t.Fatalf("foreign owner resumed: %v", err)
	}
}
