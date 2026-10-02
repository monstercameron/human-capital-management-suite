package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// AgentAnnouncementScheduleBindingSource resolves the published agent,
// service principal, audience and budget under current installation authority.
// None of these pins may be supplied by the announcement form.
type AgentAnnouncementScheduleBindingSource interface {
	ResolveAnnouncementScheduleBinding(context.Context, AgentAnnouncementActor, agentstore.Announcement) (scheduled.Schedule, error)
}

type AgentAnnouncementNativeSchedules struct {
	Store    scheduled.OwnerStore
	Bindings AgentAnnouncementScheduleBindingSource
	Now      func() time.Time
}

func (s AgentAnnouncementNativeSchedules) definition(ctx context.Context, actor AgentAnnouncementActor, record agentstore.Announcement, cron string, expected uint64) (scheduled.Schedule, error) {
	if s.Store == nil || s.Bindings == nil || s.Now == nil || ctx == nil || actor.SubjectID != record.OwnerID {
		return scheduled.Schedule{}, ErrAgentAnnouncementDenied
	}
	base, err := s.Bindings.ResolveAnnouncementScheduleBinding(ctx, actor, record)
	if err != nil {
		return scheduled.Schedule{}, err
	}
	d := base.Trigger.Definition
	if d.AgentRun == nil || d.TenantID != actor.TenantID || base.InstallationID != record.InstallationID || base.OwnerID != actor.SubjectID || base.AgentPrincipalID == "" {
		return scheduled.Schedule{}, ErrAgentAnnouncementDenied
	}
	d.ID, d.Owner, d.Version = record.SchedulerID, actor.SubjectID, strconv.FormatUint(expected+1, 10)
	d.Source = schedule.TriggerSource{Kind: schedule.SourceCron, Cron: schedule.CronSource{Expression: cron}}
	d.Overlap = schedule.OverlapSkip
	d.OverlapPolicy = ""
	data, err := json.Marshal([]any{record.InstallationID, record.PersonaID, record.ConversationID, record.Instruction, record.Documents, record.Zone})
	if err != nil {
		return scheduled.Schedule{}, err
	}
	hash := sha256.Sum256(data)
	d.InputTemplateDigest = "sha256:" + hex.EncodeToString(hash[:])
	base.Context.ID, base.Context.SnapshotID, base.Context.Digest = record.ID, strconv.FormatUint(record.Revision, 10), announcementDefinitionDigest(record)
	base.Zone.ID = record.Zone
	base.Misfire = schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, MaxCatchUp: 1}
	base.DST = "EARLIER"
	base.State, base.Revision, base.Cursor = scheduled.StateActive, expected+1, values.NewInstant(s.Now().UTC())
	base.Trigger, err = schedule.Publish(schedule.NewRegistry(), d, []schedule.AuthorizedTarget{{AgentRun: d.AgentRun}})
	if err != nil {
		return scheduled.Schedule{}, err
	}
	return base, scheduled.ValidateSchedule(base)
}

func (s AgentAnnouncementNativeSchedules) CreateAnnouncementSchedule(ctx context.Context, actor AgentAnnouncementActor, record agentstore.Announcement, cron string) (string, *time.Time, error) {
	if cron == "" {
		return record.SchedulerID, nil, nil
	}
	current, found, err := s.Store.Load(ctx, actor.TenantID, record.SchedulerID)
	if err != nil {
		return "", nil, err
	}
	if found {
		return "", nil, agentstore.ErrAnnouncementRevision
	}
	current, err = s.definition(ctx, actor, record, cron, 0)
	if err != nil {
		return "", nil, err
	}
	next, err := announcementNativeNext(current, s.Now())
	if err != nil {
		return "", nil, err
	}
	err = s.Store.Save(ctx, current, 0, scheduled.Audit{TenantID: actor.TenantID, ScheduleID: record.SchedulerID, ActorID: actor.SubjectID, Revision: 1, Action: scheduled.ActionPublish, At: s.Now().UTC()})
	return record.SchedulerID, next, err
}

func (s AgentAnnouncementNativeSchedules) UpdateAnnouncementSchedule(ctx context.Context, actor AgentAnnouncementActor, record agentstore.Announcement, cron string) (*time.Time, error) {
	prior, found, err := s.Store.Load(ctx, actor.TenantID, record.SchedulerID)
	if err != nil {
		return nil, err
	}
	if !found {
		_, next, err := s.CreateAnnouncementSchedule(ctx, actor, record, cron)
		return next, err
	}
	if prior.OwnerID != actor.SubjectID {
		return nil, ErrAgentAnnouncementDenied
	}
	if cron == "" {
		return nil, s.DeleteAnnouncementSchedule(ctx, actor, record.SchedulerID, prior.Revision)
	}
	current, err := s.definition(ctx, actor, record, cron, prior.Revision)
	if err != nil {
		return nil, err
	}
	if record.State == agentstore.AnnouncementPaused {
		current.State = scheduled.StatePaused
	}
	next, err := announcementNativeNext(current, s.Now())
	if err != nil {
		return nil, err
	}
	err = s.Store.Save(ctx, current, prior.Revision, scheduled.Audit{TenantID: actor.TenantID, ScheduleID: record.SchedulerID, ActorID: actor.SubjectID, Revision: current.Revision, Action: scheduled.ActionDraft, At: s.Now().UTC()})
	return next, err
}

func (s AgentAnnouncementNativeSchedules) state(ctx context.Context, actor AgentAnnouncementActor, id, state string) (*time.Time, error) {
	current, found, err := s.Store.Load(ctx, actor.TenantID, id)
	if err != nil || !found {
		return nil, err
	}
	if current.OwnerID != actor.SubjectID {
		return nil, ErrAgentAnnouncementDenied
	}
	expected := current.Revision
	current.State, current.Revision = state, expected+1
	if projected, ok := ctx.Value(announcementDraftKey{}).(agentstore.Announcement); ok && projected.ID == current.Context.ID && projected.OwnerID == actor.SubjectID && projected.TenantKey == actor.TenantID {
		current.Context.SnapshotID = strconv.FormatUint(projected.Revision, 10)
		current.Context.Digest = announcementDefinitionDigest(projected)
	}
	action := scheduled.ActionPause
	if state == scheduled.StateActive {
		action = scheduled.ActionResume
	} else if state == scheduled.StateRetired {
		action = scheduled.ActionRetire
	}
	next, err := announcementNativeNext(current, s.Now())
	if err != nil {
		return nil, err
	}
	err = s.Store.Save(ctx, current, expected, scheduled.Audit{TenantID: actor.TenantID, ScheduleID: id, ActorID: actor.SubjectID, Revision: current.Revision, Action: action, At: s.Now().UTC()})
	return next, err
}

func (s AgentAnnouncementNativeSchedules) PauseAnnouncementSchedule(ctx context.Context, actor AgentAnnouncementActor, id string, _ uint64) error {
	_, err := s.state(ctx, actor, id, scheduled.StatePaused)
	return err
}
func (s AgentAnnouncementNativeSchedules) ResumeAnnouncementSchedule(ctx context.Context, actor AgentAnnouncementActor, id string, _ uint64) (*time.Time, error) {
	return s.state(ctx, actor, id, scheduled.StateActive)
}
func (s AgentAnnouncementNativeSchedules) DeleteAnnouncementSchedule(ctx context.Context, actor AgentAnnouncementActor, id string, _ uint64) error {
	_, err := s.state(ctx, actor, id, scheduled.StateRetired)
	return err
}

func (s AgentAnnouncementNativeSchedules) NextAnnouncementRun(ctx context.Context, actor AgentAnnouncementActor, record agentstore.Announcement) (*time.Time, error) {
	if s.Store == nil || s.Now == nil || ctx == nil || actor.SubjectID != record.OwnerID {
		return nil, ErrAgentAnnouncementDenied
	}
	current, found, err := s.Store.Load(ctx, actor.TenantID, record.SchedulerID)
	if err != nil {
		return nil, err
	}
	if !found || current.OwnerID != actor.SubjectID || current.InstallationID != record.InstallationID {
		return nil, ErrAgentAnnouncementDenied
	}
	return announcementNativeNext(current, s.Now().Add(time.Nanosecond))
}

func announcementNativeNext(current scheduled.Schedule, at time.Time) (*time.Time, error) {
	if current.State != scheduled.StateActive {
		return nil, nil
	}
	result, err := schedule.CalculateOccurrences(current.Trigger, schedule.OccurrenceRequest{Window: schedule.OccurrenceWindow{Start: values.NewInstant(at), End: values.NewInstant(at.AddDate(1, 0, 0))}, Zone: current.Zone, Calendar: current.Calendar, Misfire: current.Misfire})
	if err != nil {
		return nil, err
	}
	result, err = scheduled.ApplyDST(current, result)
	if err != nil || len(result.Occurrences) == 0 {
		return nil, err
	}
	next := result.Occurrences[0].At.Time()
	return &next, nil
}

var _ AgentAnnouncementScheduleOwner = AgentAnnouncementNativeSchedules{}
