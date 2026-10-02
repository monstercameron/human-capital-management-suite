package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
)

// Settle refused output so overlap prevention does not block the next week.
func (r *AgentAnnouncementRuntime) RefuseAnnouncementOutput(ctx context.Context, output agentsecurity.FinalOutputPersistence) error {
	record, run, store, err := r.outputRun(ctx, output)
	if err != nil {
		return err
	}
	if run.State != runstate.StateRunning {
		return nil
	}
	repo, err := r.admissionRepository(record.Request.Source.TenantID)
	if err != nil {
		return err
	}
	recheck, err := NewPersonaRunAdmissionRechecker(record.Request.Source.TenantID, repo, r)
	if err != nil {
		return err
	}
	state, err := runstate.New(store, recheck)
	if err != nil {
		return err
	}
	if !run.Deadline.After(r.Now()) {
		_, err = state.Expire(ctx, run.ID, run.Version, r.Now())
		return err
	}
	_, err = state.Fail(ctx, run.ID, r.Base.WorkerID, "DELIVERY_REFUSED", false, run.Fence, run.Version, r.Now())
	return err
}
