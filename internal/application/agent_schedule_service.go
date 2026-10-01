package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agenttriggerstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type AgentScheduleServiceConfig struct {
	CoreDB                  dbport.Beginner
	SourceDB, SourceFenceDB dbport.Beginner
	Agents                  *agentstore.Store
	TenantUUID              func(values.TenantId) uuid.UUID
	Now                     func() time.Time
}

// AgentScheduleService is both the authenticated schedule control surface and
// native source owner for shared agent admission and execution fences.
type AgentScheduleService struct {
	cfg      AgentScheduleServiceConfig
	mu       sync.RWMutex
	inbox    scheduled.Inbox
	bindings interface {
		CheckCurrent(context.Context, agentrun.Request) error
	}
}

func NewAgentScheduleService(cfg AgentScheduleServiceConfig) (*AgentScheduleService, error) {
	if cfg.CoreDB == nil || cfg.SourceDB == nil || cfg.SourceFenceDB == nil || cfg.Agents == nil || cfg.TenantUUID == nil || cfg.Now == nil {
		return nil, agentcontrols.ErrUnavailable
	}
	return &AgentScheduleService{cfg: cfg}, nil
}

// BindCommon completes the composition cycle before handlers/ticks are started.
func (s *AgentScheduleService) BindCommon(inbox scheduled.Inbox, bindings interface {
	CheckCurrent(context.Context, agentrun.Request) error
}) error {
	if s == nil || inbox == nil || bindings == nil {
		return agentcontrols.ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inbox = inbox
	s.bindings = bindings
	return nil
}
func (s *AgentScheduleService) ports(tenant string) (*agenttriggerstore.Store, *scheduled.Owner, *scheduled.OutboxWorker, error) {
	if s == nil {
		return nil, nil, nil, agentcontrols.ErrUnavailable
	}
	s.mu.RLock()
	inbox, bindings := s.inbox, s.bindings
	s.mu.RUnlock()
	if inbox == nil || bindings == nil {
		return nil, nil, nil, agentcontrols.ErrUnavailable
	}
	store, err := agenttriggerstore.NewScheduleSource(agenttriggerstore.ScheduleSourceRunner{DB: s.cfg.SourceDB, FenceDB: s.cfg.SourceFenceDB}, s.cfg.TenantUUID(values.TenantId(tenant)), tenant, agenttriggerstore.AgentScheduleExecutions{Database: s.cfg.Agents})
	if err != nil {
		return nil, nil, nil, err
	}
	authority := agentScheduleAuthority{service: s, bindings: bindings}
	owner, err := scheduled.NewOwner(store, authority)
	if err != nil {
		return nil, nil, nil, err
	}
	worker, err := scheduled.NewOutboxWorker(owner, store, agentScheduleContext{}, inbox)
	return store, owner, worker, err
}

type agentScheduleContext struct{}

func (agentScheduleContext) BuildScheduleContext(_ context.Context, s scheduled.Schedule, _ schedule.Occurrence) (agentrun.ContextScope, error) {
	if s.Context.ID == "" || s.Context.SnapshotID == "" || !personaRunAuthorityDigest(s.Context.Digest) {
		return agentrun.ContextScope{}, agentrun.ErrAuthorityMissing
	}
	return s.Context, nil
}
func (s *AgentScheduleService) actor(ctx context.Context) (scheduled.Actor, error) {
	if s == nil {
		return scheduled.Actor{}, agentcontrols.ErrUnavailable
	}
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil {
		return scheduled.Actor{}, agentcontrols.ErrUnauthenticated
	}
	if p.SubjectKind() != trust.SubjectKindHuman || !p.ExpiresAt().After(s.cfg.Now()) {
		return scheduled.Actor{}, agentcontrols.ErrDenied
	}
	return scheduled.Actor{TenantID: p.Tenant().String(), UserID: p.Subject()}, nil
}
func (s *AgentScheduleService) CheckRequest(ctx context.Context, r agentrun.Request) error {
	_, _, worker, err := s.ports(r.Source.TenantID)
	if err != nil {
		return err
	}
	return worker.CheckRequest(ctx, r)
}
func (s *AgentScheduleService) SourceDelivery(ctx context.Context, tenant, key string) (scheduled.Delivery, error) {
	store, _, _, err := s.ports(tenant)
	if err != nil {
		return scheduled.Delivery{}, err
	}
	delivery, found, err := store.GetDelivery(ctx, tenant, key)
	if err != nil {
		return scheduled.Delivery{}, err
	}
	if !found {
		return scheduled.Delivery{}, scheduled.ErrInvalidFiring
	}
	return delivery, nil
}
func (s *AgentScheduleService) WithExecutionFence(ctx context.Context, r agentrun.Request, claim func(error) (runstate.Run, error)) (runstate.Run, error) {
	store, _, _, err := s.ports(r.Source.TenantID)
	if err != nil {
		return runstate.Run{}, err
	}
	return store.WithExecutionFence(ctx, r.Source.TenantID, r.Source.Ref, func(policyError error) (runstate.Run, error) {
		switch {
		case errors.Is(policyError, scheduled.ErrOverlapQueued):
			policyError = &CommonAgentExecutionDeferred{Code: "SCHEDULE_OVERLAP_QUEUED"}
		case errors.Is(policyError, scheduled.ErrOverlapSkipped):
			policyError = &CommonAgentExecutionRefused{Code: "SCHEDULE_OVERLAP_SKIPPED"}
		case errors.Is(policyError, scheduled.ErrOverlapRefused):
			policyError = &CommonAgentExecutionRefused{Code: "SCHEDULE_OVERLAP_REFUSED"}
		}
		return claim(policyError)
	})
}
func (s *AgentScheduleService) ResolveSourceKey(ctx context.Context, r agentrun.Request) (string, error) {
	_, _, worker, err := s.ports(r.Source.TenantID)
	if err != nil {
		return "", err
	}
	return worker.ResolveSourceKey(ctx, r)
}
func (s *AgentScheduleService) ReplayPending(ctx context.Context, tenant string, limit int) (int, error) {
	_, _, worker, err := s.ports(tenant)
	if err != nil {
		return 0, err
	}
	done, replayErr := worker.Replay(ctx, tenant, limit)
	repaired, repairErr := s.reconcileAcknowledged(ctx, tenant, limit)
	return done + repaired, errors.Join(replayErr, repairErr)
}
func (s *AgentScheduleService) TickTenant(ctx context.Context, tenant string) error {
	store, _, worker, err := s.ports(tenant)
	if err != nil {
		return err
	}
	list, err := store.List(ctx, tenant)
	if err != nil {
		return err
	}
	var failures []error
	for _, record := range list {
		if record.State != scheduled.StateActive {
			continue
		}
		if err := worker.Plan(ctx, tenant, record.Trigger.Definition.ID, s.cfg.Now()); err != nil {
			failures = append(failures, err)
		}
	}
	if _, err := worker.Replay(ctx, tenant, 100); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}
func (s *AgentScheduleService) Snapshot(ctx context.Context) (agentcontrols.Reply, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return agentcontrols.Reply{}, err
	}
	store, owner, _, err := s.ports(actor.TenantID)
	if err != nil {
		return agentcontrols.Reply{}, err
	}
	list, err := store.List(ctx, actor.TenantID)
	if err != nil {
		return agentcontrols.Reply{}, err
	}
	reply := agentcontrols.Reply{Snapshot: productui.AgentControlsSnapshot{Available: true}}
	for _, record := range list {
		scope, err := s.managementScope(ctx, actor, record.Trigger.Definition.ID)
		if err != nil {
			if errors.Is(err, agentcontrols.ErrDenied) {
				continue
			}
			return agentcontrols.Reply{}, err
		}
		if !agentScheduleScopeMatches(scope, record) {
			continue
		}
		row := agentScheduleRow(record)
		for _, action := range scope.Actions {
			if action == "DRAFT" {
				reply.Snapshot.CanDraft = true
			}
			if action == "PUBLISH" && (record.State != scheduled.StateDraft || actor.UserID == record.OwnerID) {
				continue
			}
			if action == "PAUSE" && record.State != scheduled.StateActive || action == "RESUME" && record.State != scheduled.StatePaused || action == "SKIP" && record.State != scheduled.StateActive || action == "DRY_RUN" && record.State != scheduled.StateActive {
				continue
			}
			row.Actions = append(row.Actions, strings.ToLower(action))
		}
		at := s.cfg.Now()
		if result, err := owner.Preview(ctx, actor, record, schedule.OccurrenceWindow{Start: values.NewInstant(at), End: values.NewInstant(at.Add(7 * 24 * time.Hour))}); err == nil {
			for _, occ := range result.Occurrences {
				row.Occurrences = append(row.Occurrences, occ.At.String())
				row.OccurrenceKeys = append(row.OccurrenceKeys, occ.Key)
			}
		}
		reply.Snapshot.Schedules = append(reply.Snapshot.Schedules, row)
	}
	// A draft grant can exist before any schedule row is created.
	grants, err := s.managementScopes(ctx, actor)
	if err != nil {
		return agentcontrols.Reply{}, err
	}
	for _, scope := range grants {
		for _, action := range scope.Actions {
			if action == "DRAFT" {
				reply.Snapshot.CanDraft = true
			}
		}
	}
	return reply, nil
}
func agentScheduleRow(s scheduled.Schedule) productui.AgentControlSchedule {
	d := s.Trigger.Definition
	return productui.AgentControlSchedule{ID: d.ID, Revision: s.Revision, State: s.State, Version: d.AgentRun.Agent.Version, Installation: s.InstallationID, Recurrence: d.Source.Cron.Expression, DST: s.DST, Zone: s.Zone.ID, Calendar: s.Calendar.Calendar.String(), Destination: d.AgentRun.Destination.AudienceID, Misfire: string(s.Misfire.Policy), Overlap: string(d.Overlap), Budget: fmt.Sprintf("%d µcost / %d input / %d output", d.AgentRun.Budget.MaxCostMicros, d.AgentRun.Budget.MaxInputTokens, d.AgentRun.Budget.MaxOutputTokens)}
}
func (s *AgentScheduleService) Control(ctx context.Context, c productui.AgentControlsCommand) (agentcontrols.Reply, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return agentcontrols.Reply{}, err
	}
	if c.Kind != "schedule" || c.IdempotencyKey == "" || c.Reason == "" {
		return agentcontrols.Reply{}, agentcontrols.ErrInvalid
	}
	_, owner, _, err := s.ports(actor.TenantID)
	if err != nil {
		return agentcontrols.Reply{}, err
	}
	action := strings.ToUpper(c.Action)
	record, err := owner.Control(ctx, actor, c.ID, c.ExpectedRevision, scheduled.Action(action), c.Occurrence, s.cfg.Now())
	if err != nil {
		return agentcontrols.Reply{}, agentScheduleError(err)
	}
	if action == "DRY_RUN" {
		result, err := owner.Preview(ctx, actor, record, schedule.OccurrenceWindow{Start: values.NewInstant(s.cfg.Now()), End: values.NewInstant(s.cfg.Now().Add(7 * 24 * time.Hour))})
		if err != nil {
			return agentcontrols.Reply{}, agentScheduleError(err)
		}
		row := agentScheduleRow(record)
		for _, occ := range result.Occurrences {
			row.Occurrences = append(row.Occurrences, occ.At.String())
			row.OccurrenceKeys = append(row.OccurrenceKeys, occ.Key)
		}
		return agentcontrols.Reply{Snapshot: productui.AgentControlsSnapshot{Available: true, Schedules: []productui.AgentControlSchedule{row}}}, nil
	}
	return s.Snapshot(ctx)
}
func (s *AgentScheduleService) Draft(ctx context.Context, input productui.AgentScheduleDraft) (agentcontrols.Reply, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return agentcontrols.Reply{}, err
	}
	record, err := s.resolveDraft(ctx, actor, input)
	if err != nil {
		return agentcontrols.Reply{}, err
	}
	_, owner, _, err := s.ports(actor.TenantID)
	if err != nil {
		return agentcontrols.Reply{}, err
	}
	if _, err := owner.Draft(ctx, actor, record, input.ExpectedRevision, s.cfg.Now()); err != nil {
		return agentcontrols.Reply{}, agentScheduleError(err)
	}
	return s.Snapshot(ctx)
}
func (s *AgentScheduleService) Preview(ctx context.Context, input productui.AgentScheduleDraft) (agentcontrols.Reply, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return agentcontrols.Reply{}, err
	}
	record, err := s.resolveDraft(ctx, actor, input)
	if err != nil {
		return agentcontrols.Reply{}, err
	}
	_, owner, _, err := s.ports(actor.TenantID)
	if err != nil {
		return agentcontrols.Reply{}, err
	}
	result, err := owner.Preview(ctx, actor, record, schedule.OccurrenceWindow{Start: values.NewInstant(s.cfg.Now()), End: values.NewInstant(s.cfg.Now().Add(7 * 24 * time.Hour))})
	if err != nil {
		return agentcontrols.Reply{}, agentScheduleError(err)
	}
	row := agentScheduleRow(record)
	for _, occ := range result.Occurrences {
		row.Occurrences = append(row.Occurrences, occ.At.String())
		row.OccurrenceKeys = append(row.OccurrenceKeys, occ.Key)
	}
	return agentcontrols.Reply{Snapshot: productui.AgentControlsSnapshot{Available: true, Schedules: []productui.AgentControlSchedule{row}}}, nil
}
func agentScheduleError(err error) error {
	switch {
	case errors.Is(err, scheduled.ErrRevision):
		return errors.Join(agentcontrols.ErrConflict, err)
	case errors.Is(err, scheduled.ErrAuthority):
		return errors.Join(agentcontrols.ErrDenied, err)
	case errors.Is(err, scheduled.ErrInvalidSchedule), errors.Is(err, scheduled.ErrInactive), errors.Is(err, schedule.ErrRejected):
		return errors.Join(agentcontrols.ErrInvalid, err)
	}
	return err
}
func agentSchedulePublicationVersion(expected uint64) string {
	return strconv.FormatUint(expected+1, 10)
}
