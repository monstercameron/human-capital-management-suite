package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
)

// CommonAgentAdmissionStore is the durable tenant inbox and recovery reader.
type CommonAgentAdmissionStore interface {
	agentrun.Store
	GetByID(context.Context, string) (agentrun.Record, error)
}

type commonAgentAdmissionLister interface {
	ListBySource(context.Context, agentrun.SourceKind, int) ([]agentrun.Record, error)
}

type commonAgentPendingLister interface {
	ListPendingBySource(context.Context, agentrun.SourceKind, int) ([]agentrun.Record, error)
}

// CommonAgentExecutionFence holds the source owner's durable occurrence
// fence while checking overlap and claiming the common execution metadata.
type CommonAgentExecutionFence interface {
	WithExecutionFence(context.Context, agentrun.Request, func(error) (runstate.Run, error)) (runstate.Run, error)
}

// ErrCommonAgentExecutionDeferred marks a retryable source queue disposition.
var ErrCommonAgentExecutionDeferred = errors.New("application: agent execution deferred by source policy")

// CommonAgentExecutionDeferred leaves the accepted execution READY for retry.
type CommonAgentExecutionDeferred struct{ Code string }

func (e *CommonAgentExecutionDeferred) Error() string {
	return ErrCommonAgentExecutionDeferred.Error() + ": " + e.Code
}
func (e *CommonAgentExecutionDeferred) Unwrap() error { return ErrCommonAgentExecutionDeferred }

// CommonAgentExecutionRefused consumes an occurrence as an explicit skip or
// refusal before a model lease or capability effect is acquired.
type CommonAgentExecutionRefused struct{ Code string }

func (e *CommonAgentExecutionRefused) Error() string {
	return agentrun.ErrAuthorityRefusal.Error() + ": " + e.Code
}
func (e *CommonAgentExecutionRefused) Unwrap() error { return agentrun.ErrAuthorityRefusal }

// CommonAgentTenantStores returns separately isolated inbox and execution
// repositories. The adapter never resolves a run from a global ID alone.
type CommonAgentTenantStores interface {
	ForTenant(context.Context, string) (CommonAgentAdmissionStore, runstate.Store, error)
}

// CommonAgentRuntimeConfig composes current authority with durable storage.
type CommonAgentRuntimeConfig struct {
	Stores          CommonAgentTenantStores
	Authority       agentrun.Authority
	Now             func() time.Time
	ExecutionFences map[agentrun.SourceKind]CommonAgentExecutionFence
}

// CommonAgentRuntime admits all configured entry points into one durable
// inbox and execution state machine. Admit performs no inference and holds no
// workflow worker lease. Claim is the worker's current-authority boundary.
type CommonAgentRuntime struct{ cfg CommonAgentRuntimeConfig }

func NewCommonAgentRuntime(cfg CommonAgentRuntimeConfig) (*CommonAgentRuntime, error) {
	if cfg.Stores == nil || cfg.Authority == nil || cfg.Now == nil {
		return nil, agentrun.ErrAuthorityMissing
	}
	if cfg.ExecutionFences != nil {
		fences := make(map[agentrun.SourceKind]CommonAgentExecutionFence, len(cfg.ExecutionFences))
		for source, fence := range cfg.ExecutionFences {
			if fence == nil {
				return nil, agentrun.ErrAuthorityMissing
			}
			fences[source] = fence
		}
		cfg.ExecutionFences = fences
	}
	return &CommonAgentRuntime{cfg: cfg}, nil
}

// Admit preserves the source owner's dedupe identity. Acceptance creates
// durable READY work; replay repairs the gap if a process stopped between the
// immutable inbox insert and execution insert.
func (r *CommonAgentRuntime) Admit(ctx context.Context, request agentrun.Request) (agentrun.Record, bool, error) {
	inbox, executions, err := r.stores(ctx, request.Source.TenantID)
	if err != nil {
		return agentrun.Record{}, false, err
	}
	service, err := agentrun.NewAdmissionService(agentrun.AdmissionConfig{Authority: r.cfg.Authority, Store: inbox, Now: r.cfg.Now})
	if err != nil {
		return agentrun.Record{}, false, err
	}
	record, created, err := service.Admit(ctx, request)
	if err != nil || record.Decision != agentrun.DecisionAccepted {
		return record, created, err
	}
	_, err = r.ensureExecution(ctx, executions, record)
	return record, created, err
}

func (r *CommonAgentRuntime) ensureExecution(ctx context.Context, store runstate.Store, record agentrun.Record) (runstate.Run, error) {
	existing, err := store.Get(ctx, record.ID)
	if err == nil {
		return commonAgentCheckExecution(existing, record)
	}
	if !errors.Is(err, agentrunstate.ErrNotFound) && !errors.Is(err, runstate.ErrNotFound) {
		return runstate.Run{}, err
	}
	service, err := runstate.New(store, r)
	if err != nil {
		return runstate.Run{}, err
	}
	run, err := service.Start(ctx, record)
	if err == nil {
		return run, nil
	}
	// A concurrent replay can win the unique execution insert. Reloading and
	// comparing the admission pins distinguishes it from a storage failure.
	existing, readErr := store.Get(ctx, record.ID)
	if readErr != nil {
		return runstate.Run{}, err
	}
	return commonAgentCheckExecution(existing, record)
}

func commonAgentCheckExecution(run runstate.Run, record agentrun.Record) (runstate.Run, error) {
	if run.ID != record.ID || run.AdmissionID != record.ID || run.TenantID != record.Request.Source.TenantID ||
		run.RequestDigest != record.RequestDigest || run.AgentID != record.Authority.Agent.AgentID ||
		run.AgentVersion != record.Authority.Agent.Version || run.AgentDigest != record.Authority.Agent.Digest ||
		run.ContextDigest != record.Authority.Context.Digest || !run.Deadline.Equal(record.Request.Deadline) {
		return runstate.Run{}, agentrun.ErrSourceConflict
	}
	return run, nil
}

func (r *CommonAgentRuntime) GetAdmission(ctx context.Context, tenant, id string) (agentrun.Record, error) {
	inbox, _, err := r.stores(ctx, tenant)
	if err != nil {
		return agentrun.Record{}, err
	}
	record, err := inbox.GetByID(ctx, id)
	if err != nil {
		return agentrun.Record{}, err
	}
	if record.ID != id || record.Request.Source.TenantID != tenant || agentrun.ValidateAdmissionRecord(record) != nil {
		return agentrun.Record{}, agentrun.ErrSourceConflict
	}
	return record, nil
}

func (r *CommonAgentRuntime) GetRun(ctx context.Context, tenant, id string) (runstate.Run, error) {
	record, err := r.GetAdmission(ctx, tenant, id)
	if err != nil {
		return runstate.Run{}, err
	}
	_, executions, err := r.stores(ctx, tenant)
	if err != nil {
		return runstate.Run{}, err
	}
	run, err := executions.Get(ctx, id)
	if err != nil {
		return runstate.Run{}, err
	}
	return commonAgentCheckExecution(run, record)
}

// Recheck reloads durable request references and resolves fresh authority;
// the old acceptance snapshot is never used as a permission grant.
func (r *CommonAgentRuntime) Recheck(ctx context.Context, tenant, id string) error {
	record, err := r.GetAdmission(ctx, tenant, id)
	if err != nil {
		return err
	}
	if record.Decision != agentrun.DecisionAccepted || !record.Request.Deadline.After(r.cfg.Now().UTC()) {
		return commonAgentRefusal("ADMISSION_NOT_CURRENT")
	}
	snapshot, err := r.cfg.Authority.VerifyAdmission(ctx, record.Request)
	if err != nil {
		return err
	}
	if snapshot.Agent != record.Authority.Agent || snapshot.InstallationID != record.Authority.InstallationID ||
		snapshot.Principal != record.Authority.Principal || snapshot.Context != record.Authority.Context || snapshot.Audience != record.Authority.Audience ||
		!personaRunBudgetWithin(record.Request.Budget, snapshot.BudgetCeiling) || snapshot.GrantRef == "" || !personaRunAuthorityDigest(snapshot.PolicyDigest) {
		return commonAgentRefusal("AUTHORITY_CHANGED")
	}
	return nil
}

// Claim obtains a fenced lease only after reloading the current source,
// principal and sponsor. Consumers drive model and capability work under the
// returned lease through the existing owner gateways.
func (r *CommonAgentRuntime) Claim(ctx context.Context, tenant, id, worker string, ttl time.Duration) (runstate.Run, error) {
	if _, err := r.GetRun(ctx, tenant, id); err != nil {
		return runstate.Run{}, err
	}
	service, err := r.executionService(ctx, tenant)
	if err != nil {
		return runstate.Run{}, err
	}
	record, err := r.GetAdmission(ctx, tenant, id)
	if err != nil {
		return runstate.Run{}, err
	}
	claim := func() (runstate.Run, error) { return service.Claim(ctx, id, worker, r.cfg.Now().UTC(), ttl) }
	fence := r.cfg.ExecutionFences[record.Request.Source.Kind]
	if fence == nil {
		if record.Request.Source.Kind == agentrun.SourceSchedule {
			return runstate.Run{}, commonAgentRefusal("SCHEDULE_EXECUTION_FENCE_REQUIRED")
		}
		return claim()
	}
	return fence.WithExecutionFence(ctx, record.Request, func(policyErr error) (runstate.Run, error) {
		if policyErr == nil {
			return claim()
		}
		var refused *CommonAgentExecutionRefused
		if !errors.As(policyErr, &refused) {
			return runstate.Run{}, policyErr
		}
		// Consume this occurrence under the owner's lock without borrowing an
		// inference lease. CAS prevents overwriting concurrent execution progress.
		return r.refuseExecution(ctx, tenant, id, refused)
	})
}

func (r *CommonAgentRuntime) refuseExecution(ctx context.Context, tenant, id string, refusal *CommonAgentExecutionRefused) (runstate.Run, error) {
	if refusal == nil || (refusal.Code != "SCHEDULE_OVERLAP_SKIPPED" && refusal.Code != "SCHEDULE_OVERLAP_REFUSED") {
		return runstate.Run{}, agentrun.ErrInvalidRequest
	}
	run, err := r.GetRun(ctx, tenant, id)
	if err != nil {
		return runstate.Run{}, err
	}
	if run.State != runstate.StateReady {
		return runstate.Run{}, runstate.ErrConflict
	}
	_, store, err := r.stores(ctx, tenant)
	if err != nil {
		return runstate.Run{}, err
	}
	expected := run.Version
	run.Version++
	run.UpdatedAt, run.TerminalCode = r.cfg.Now().UTC(), refusal.Code
	if refusal.Code == "SCHEDULE_OVERLAP_SKIPPED" {
		run.State, run.CancelRequested = runstate.StateCancelled, true
	} else {
		run.State, run.FailureRequested, run.Retryable = runstate.StateFailed, true, false
	}
	if err := store.Save(ctx, run, expected); err != nil {
		return runstate.Run{}, err
	}
	return run, refusal
}

// Pending reconstructs unfinished work from the durable inbox, including an
// accepted record whose execution insert was interrupted. Workers can poll it
// after a restart; no process-local enqueue is needed to preserve a firing.
func (r *CommonAgentRuntime) Pending(ctx context.Context, tenant string, source agentrun.SourceKind, limit int) ([]runstate.Run, error) {
	inbox, store, err := r.stores(ctx, tenant)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		return nil, agentrun.ErrInvalidRequest
	}
	var records []agentrun.Record
	if pending, ok := inbox.(commonAgentPendingLister); ok {
		records, err = pending.ListPendingBySource(ctx, source, limit)
	} else {
		records, err = r.ListAdmissions(ctx, tenant, source, limit)
	}
	if err != nil {
		return nil, err
	}
	var pending []runstate.Run
	for _, record := range records {
		if record.Request.Source.TenantID != tenant || record.Request.Source.Kind != source || agentrun.ValidateAdmissionRecord(record) != nil {
			return nil, agentrun.ErrSourceConflict
		}
		if record.Decision != agentrun.DecisionAccepted {
			continue
		}
		run, err := r.ensureExecution(ctx, store, record)
		if err != nil {
			return nil, err
		}
		if run.State == runstate.StateReady || run.State == runstate.StateRunning || run.State == runstate.StateReconciling {
			pending = append(pending, run)
		}
	}
	return pending, nil
}

// ListAdmissions returns tenant-scoped durable decisions for source-owner
// completion reconciliation, including refusals and terminal executions.
func (r *CommonAgentRuntime) ListAdmissions(ctx context.Context, tenant string, source agentrun.SourceKind, limit int) ([]agentrun.Record, error) {
	inbox, _, err := r.stores(ctx, tenant)
	if err != nil {
		return nil, err
	}
	lister, ok := inbox.(commonAgentAdmissionLister)
	if !ok || limit < 1 || limit > 1000 {
		return nil, agentrun.ErrInvalidRequest
	}
	records, err := lister.ListBySource(ctx, source, limit)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if record.Request.Source.TenantID != tenant || record.Request.Source.Kind != source || agentrun.ValidateAdmissionRecord(record) != nil {
			return nil, agentrun.ErrSourceConflict
		}
	}
	return records, nil
}

// ExecutionService exposes the existing fenced transition service with this
// runtime's fresh admission rechecker for model, tool and delivery checkpoints.
func (r *CommonAgentRuntime) ExecutionService(ctx context.Context, tenant string) (*runstate.Service, error) {
	return r.executionService(ctx, tenant)
}

// Recover releases an expired lease or repairs a missing execution from its
// accepted inbox record. Ambiguous capability effects remain RECONCILING.
func (r *CommonAgentRuntime) Recover(ctx context.Context, tenant, id string) (runstate.Run, error) {
	record, err := r.GetAdmission(ctx, tenant, id)
	if err != nil || record.Decision != agentrun.DecisionAccepted {
		if err == nil {
			err = commonAgentRefusal("ADMISSION_REFUSED")
		}
		return runstate.Run{}, err
	}
	_, store, err := r.stores(ctx, tenant)
	if err != nil {
		return runstate.Run{}, err
	}
	run, err := r.ensureExecution(ctx, store, record)
	if err != nil {
		return run, err
	}
	service, err := runstate.New(store, r)
	if err != nil {
		return runstate.Run{}, err
	}
	now := r.cfg.Now().UTC()
	if !run.ExpireRequested && !run.Deadline.After(now) && (run.State == runstate.StateReady || run.State == runstate.StateRunning || run.State == runstate.StateWaiting || run.State == runstate.StateReconciling) {
		return service.Expire(ctx, id, run.Version, now)
	}
	if run.State != runstate.StateRunning {
		return run, nil
	}
	if run.Lease != nil && run.Lease.Until.After(now) {
		return run, nil
	}
	return service.Recover(ctx, id, run.Version, now)
}

func (r *CommonAgentRuntime) executionService(ctx context.Context, tenant string) (*runstate.Service, error) {
	_, store, err := r.stores(ctx, tenant)
	if err != nil {
		return nil, err
	}
	return runstate.New(store, r)
}

func (r *CommonAgentRuntime) stores(ctx context.Context, tenant string) (CommonAgentAdmissionStore, runstate.Store, error) {
	if r == nil || ctx == nil || r.cfg.Stores == nil || !runAuthorityCleanRequired(tenant, 256) {
		return nil, nil, agentrun.ErrAuthorityMissing
	}
	inbox, executions, err := r.cfg.Stores.ForTenant(ctx, tenant)
	if err != nil {
		return nil, nil, err
	}
	if inbox == nil || executions == nil {
		return nil, nil, fmt.Errorf("%w: tenant stores unavailable", agentrun.ErrAuthorityMissing)
	}
	return inbox, executions, nil
}
