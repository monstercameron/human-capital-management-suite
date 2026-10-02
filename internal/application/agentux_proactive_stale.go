package application

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agenttriggerstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const announcementScheduleChangedReason = "The announcement changed or was paused before this post could run. Save or resume it to schedule the next post."

type announcementStoppedAuthority struct{}

func (announcementStoppedAuthority) VerifyAdmission(context.Context, agentrun.Request) (agentrun.AuthoritySnapshot, error) {
	return agentrun.AuthoritySnapshot{}, &agentrun.AdmissionRefusal{Code: "AUTHORITY_REFUSED"}
}

// A paused or edited frozen occurrence cannot run later, and cannot hold the
// native overlap guard forever. Persist a refusal before acknowledging it.
func (w *AgentAnnouncementWorker) refuseInactiveOccurrences(ctx context.Context, store *agenttriggerstore.Store, tenant string) error {
	pending, err := store.Pending(ctx, tenant, 1000)
	if err != nil {
		return err
	}
	for _, delivery := range pending {
		if delivery.Request.Source.Kind != agentrun.SourceAnnouncement {
			continue
		}
		current, found, err := store.Load(ctx, tenant, delivery.ScheduleID)
		if err != nil {
			return err
		}
		if found && current.State == scheduled.StateActive && current.Revision == delivery.ControlRevision {
			continue
		}
		repo, err := w.Runtime.admissionRepository(tenant)
		if err != nil {
			return err
		}
		service, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: announcementStoppedAuthority{}, Store: repo, Now: w.Runtime.Now})
		if err != nil {
			return err
		}
		admission, _, err := service.Admit(ctx, delivery.Request)
		if err != nil {
			return err
		}
		tenantID := w.Runtime.Work.tenantUUID(values.TenantId(tenant))
		_, existing, err := w.Runner.Store.GetOccurrence(ctx, tenantID, delivery.Request.Context.ID, delivery.Key)
		if err != nil {
			return err
		}
		if !existing {
			if admission.Decision == agentrun.DecisionAccepted {
				if err = w.Runtime.cancelAnnouncementAdmission(ctx, admission); err != nil {
					return err
				}
			}
			_, err = w.Runner.Store.RecordOccurrence(ctx, agentstore.AnnouncementOccurrence{TenantID: tenantID.String(), AnnouncementID: delivery.Request.Context.ID, OccurrenceID: delivery.Key, Result: agentstore.AnnouncementFailed, Reason: announcementScheduleChangedReason, AttemptedAt: w.Runtime.Now().UTC()})
			if err != nil {
				return err
			}
		}
		if err = store.Acknowledge(ctx, tenant, delivery.Key, scheduled.Receipt{SourceKey: delivery.Key, RunRequestID: admission.ID, RequestDigest: admission.RequestDigest, Decision: admission.Decision, RefusalCode: admission.RefusalCode}); err != nil {
			return err
		}
	}
	return nil
}

func (r *AgentAnnouncementRuntime) cancelAnnouncementAdmission(ctx context.Context, admission agentrun.Record) error {
	factory := &DatabasePersonaRunTenantRuntimeFactory{db: r.Agents, tenantUUID: r.Work.tenantUUID, base: r.Base}
	cfg, err := factory.ForPersonaRunTenant(ctx, admission.Request.Source.TenantID)
	if err != nil {
		return err
	}
	repo, err := r.admissionRepository(admission.Request.Source.TenantID)
	if err != nil {
		return err
	}
	recheck, err := NewPersonaRunAdmissionRechecker(admission.Request.Source.TenantID, repo, r)
	if err != nil {
		return err
	}
	state, err := runstate.New(cfg.ExecutionStore, recheck)
	if err != nil {
		return err
	}
	run, err := cfg.ExecutionStore.Get(ctx, admission.ID)
	if errors.Is(err, runstate.ErrNotFound) || errors.Is(err, agentrunstate.ErrNotFound) {
		run, err = state.Start(ctx, admission)
	}
	if err != nil {
		return err
	}
	_, err = state.Cancel(ctx, run.ID, run.Version, r.Now())
	if errors.Is(err, runstate.ErrTerminal) {
		return nil
	}
	return err
}
