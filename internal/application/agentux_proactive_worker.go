package application

import (
	"context"
	"errors"
	"fmt"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agenttriggerstore"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"time"
)

type announcementScheduleStore struct{ runtime *AgentAnnouncementRuntime }

func (s announcementScheduleStore) tenant(tenant string) (*agenttriggerstore.Store, error) {
	r := s.runtime
	return agenttriggerstore.NewAnnouncementSource(r.Agents, r.Work.tenantUUID(values.TenantId(tenant)), tenant, agenttriggerstore.AgentScheduleExecutions{Database: r.Agents})
}
func (s announcementScheduleStore) Load(ctx context.Context, tenant, id string) (scheduled.Schedule, bool, error) {
	store, err := s.tenant(tenant)
	if err != nil {
		return scheduled.Schedule{}, false, err
	}
	return store.Load(ctx, tenant, id)
}
func (s announcementScheduleStore) Save(ctx context.Context, record scheduled.Schedule, expected uint64, audit scheduled.Audit) error {
	store, err := s.tenant(record.Trigger.Definition.TenantID)
	if err != nil {
		return err
	}
	return store.Save(ctx, record, expected, audit)
}

func (r *AgentAnnouncementRuntime) ResolveAnnouncementScheduleBinding(ctx context.Context, actor AgentAnnouncementActor, record agentstore.Announcement) (scheduled.Schedule, error) {
	profile, manifest, identity, policy, legal, err := r.facts(ctx, record)
	if err != nil {
		return scheduled.Schedule{}, err
	}
	audience, err := r.currentAudience(ctx, actor.TenantID, record.ConversationID)
	if err != nil {
		return scheduled.Schedule{}, err
	}
	digest, _ := manifest.Digest()
	target := &schedule.AgentRunTarget{Agent: schedule.AgentVersionRef{ID: manifest.ID, Version: fmt.Sprint(manifest.Version), Digest: digest}, SponsorID: identity.SubjectID, Purpose: personaChatReplyPurpose, Budget: schedule.AgentRunBudget{MaxCostMicros: policy.Budget.MaxCostMicros, MaxInputTokens: policy.Budget.MaxInputTokens, MaxOutputTokens: policy.Budget.MaxOutputTokens}, Destination: schedule.AgentRunDestination{AudienceID: record.ConversationID, AudienceSnapshotID: audience.SnapshotID, AudienceDigest: audience.Digest}}
	return scheduled.Schedule{SourceKind: agentrun.SourceAnnouncement, Persona: &agentrun.PersonaRef{ID: profile.PersonaID, Version: fmt.Sprint(profile.Version), Digest: mustAnnouncementProfileDigest(profile)}, RequesterID: record.OwnerID, OwnerID: record.OwnerID, InstallationID: record.InstallationID, AgentPrincipalID: identity.PrincipalID, LegalEntity: legal, Zone: values.ZoneRef{ID: record.Zone, TzdbVersion: "2026a"}, RunTimeout: policy.Deadline.Sub(r.Now()), Trigger: schedule.PublishedTrigger{Definition: schedule.TriggerDefinition{TenantID: actor.TenantID, TargetKind: schedule.TargetAgentRun, AgentRun: target, Purpose: personaChatReplyPurpose, Storm: schedule.StormPolicy{MaxFiringsPerWindow: 400, Window: 366 * 24 * time.Hour}, ExecutionMode: intent.ModeExecute, ExecutionEnvironment: intent.EnvironmentProduction}}}, nil
}

type announcementScheduleAuthority struct{ runtime *AgentAnnouncementRuntime }

func (a announcementScheduleAuthority) Authorize(ctx context.Context, actor scheduled.Actor, _ scheduled.Action, s scheduled.Schedule) ([]schedule.AuthorizedTarget, error) {
	if actor.TenantID != s.Trigger.Definition.TenantID || actor.UserID != s.OwnerID {
		return nil, scheduled.ErrAuthority
	}
	if err := a.CheckCurrent(ctx, s); err != nil {
		return nil, err
	}
	return []schedule.AuthorizedTarget{{AgentRun: s.Trigger.Definition.AgentRun}}, nil
}
func (a announcementScheduleAuthority) CheckCurrent(ctx context.Context, s scheduled.Schedule) error {
	if s.SourceKind != agentrun.SourceAnnouncement || s.Trigger.Definition.AgentRun == nil {
		return scheduled.ErrAuthority
	}
	r := a.runtime
	record, err := r.Store.Get(ctx, r.Work.tenantUUID(values.TenantId(s.Trigger.Definition.TenantID)), s.Context.ID)
	if err != nil || record.State != agentstore.AnnouncementActive || record.SchedulerID != s.Trigger.Definition.ID || fmt.Sprint(record.Revision) != s.Context.SnapshotID || announcementDefinitionDigest(record) != s.Context.Digest {
		return scheduled.ErrInactive
	}
	profile, manifest, identity, _, _, err := r.facts(ctx, record)
	if err != nil {
		return err
	}
	digest, _ := manifest.Digest()
	target := s.Trigger.Definition.AgentRun
	if s.Persona == nil || s.Persona.Digest != mustAnnouncementProfileDigest(profile) || s.AgentPrincipalID != identity.PrincipalID || target.Agent != (schedule.AgentVersionRef{ID: manifest.ID, Version: fmt.Sprint(manifest.Version), Digest: digest}) {
		return scheduled.ErrAuthority
	}
	return nil
}

type announcementScheduleInbox struct {
	runtime *AgentAnnouncementRuntime
	runner  *AgentAnnouncementRunner
}

type announcementScheduledRequestKey struct{}
type announcementScheduleContext struct{ runtime *AgentAnnouncementRuntime }

func (s announcementScheduleContext) BuildScheduleContext(_ context.Context, record scheduled.Schedule, _ schedule.Occurrence) (agentrun.ContextScope, error) {
	return record.Context, nil
}
func (s announcementScheduleContext) BuildScheduleRequest(ctx context.Context, record scheduled.Schedule, request agentrun.Request) (agentrun.Request, error) {
	if record.SourceKind != agentrun.SourceAnnouncement {
		return agentrun.Request{}, scheduled.ErrAuthority
	}
	audience, err := s.runtime.currentAudience(ctx, request.Source.TenantID, request.Audience.ID)
	if err != nil {
		return agentrun.Request{}, err
	}
	request.Audience = audience
	return request, nil
}

func (s announcementScheduleInbox) Admit(ctx context.Context, request agentrun.Request) (agentrun.Record, bool, error) {
	if request.Source.Kind != agentrun.SourceAnnouncement {
		return agentrun.Record{}, false, scheduled.ErrAuthority
	}
	ctx = context.WithValue(ctx, announcementScheduledRequestKey{}, request)
	_, err := s.runner.RunOccurrence(ctx, s.runtime.Work.tenantUUID(values.TenantId(request.Source.TenantID)), request.Context.ID, request.Source.Key, false)
	if err != nil {
		// A terminal occurrence is acknowledged even when its run was refused or
		// failed. Operational errors without a durable outcome remain retryable.
		_, found, outcomeErr := s.runtime.Store.GetOccurrence(ctx, s.runtime.Work.tenantUUID(values.TenantId(request.Source.TenantID)), request.Context.ID, request.Source.Key)
		if outcomeErr != nil || !found {
			return agentrun.Record{}, false, errors.Join(err, outcomeErr)
		}
	}
	repo, loadErr := s.runtime.admissionRepository(request.Source.TenantID)
	if loadErr != nil {
		return agentrun.Record{}, false, loadErr
	}
	id, loadErr := agentrun.AdmissionRequestID(request.Source)
	if loadErr != nil {
		return agentrun.Record{}, false, loadErr
	}
	if errors.Is(err, ErrAgentAnnouncementNotPublic) {
		service, admitErr := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: s.runtime, Store: repo, Now: s.runtime.Now})
		if admitErr != nil {
			return agentrun.Record{}, false, admitErr
		}
		// Resolution can refuse before inference admission. Persist that decision
		// too, so the existing schedule outbox can acknowledge the occurrence.
		if _, getErr := repo.GetByID(ctx, id); errors.Is(getErr, agentrunstore.ErrAdmissionNotFound) {
			return service.Admit(ctx, request)
		}
	}
	record, loadErr := repo.GetByID(ctx, id)
	return record, false, loadErr
}

type AgentAnnouncementWorker struct {
	Runtime *AgentAnnouncementRuntime
	Runner  *AgentAnnouncementRunner
}

func (w *AgentAnnouncementWorker) TickTenant(ctx context.Context, tenant string) error {
	store, err := (announcementScheduleStore{runtime: w.Runtime}).tenant(tenant)
	if err != nil {
		return err
	}
	if err := w.refuseInactiveOccurrences(ctx, store, tenant); err != nil {
		return err
	}
	owner, err := scheduled.NewOwner(store, announcementScheduleAuthority{runtime: w.Runtime})
	if err != nil {
		return err
	}
	worker, err := scheduled.NewOutboxWorker(owner, announcementOutbox{Store: store}, announcementScheduleContext{runtime: w.Runtime}, announcementScheduleInbox{runtime: w.Runtime, runner: w.Runner})
	if err != nil {
		return err
	}
	records, err := store.List(ctx, tenant)
	if err != nil {
		return err
	}
	var failures []error
	for _, record := range records {
		if record.SourceKind == agentrun.SourceAnnouncement && record.State == scheduled.StateActive && w.Runtime.Now().After(record.Cursor.Time()) {
			if err := worker.Plan(ctx, tenant, record.Trigger.Definition.ID, w.Runtime.Now()); err != nil {
				failures = append(failures, err)
			}
		}
	}
	_, err = worker.Replay(ctx, tenant, 100)
	return errors.Join(append(failures, err)...)
}

type announcementOutbox struct{ *agenttriggerstore.Store }

func (s announcementOutbox) Pending(ctx context.Context, tenant string, limit int) ([]scheduled.Delivery, error) {
	items, err := s.Store.Pending(ctx, tenant, 1000)
	if err != nil {
		return nil, err
	}
	out := make([]scheduled.Delivery, 0)
	for _, item := range items {
		if item.Request.Source.Kind == agentrun.SourceAnnouncement {
			out = append(out, item)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}
