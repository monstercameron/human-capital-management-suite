package application

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentinvocationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// personaRecoveryPolicy bounds every persona invocation. A zero field takes its
// default, so the dispatcher works unconfigured.
type personaRecoveryPolicy struct {
	// Ceiling is the longest any invocation may stay unfinished. Past it a run
	// that nobody holds is finished whatever else is true of it.
	Ceiling time.Duration
	// AdmissionGrace is how long a claim may take to reach admission. The
	// admission step is itself bounded to a minute, so a claim that is older
	// than this has lost its worker.
	AdmissionGrace time.Duration
	// Interval is the least time between two sweeps of one tenant.
	Interval time.Duration
	// StartupWindow is how far back the first sweep after a start looks.
	StartupWindow time.Duration
}

func (p personaRecoveryPolicy) filled() personaRecoveryPolicy {
	if p.Ceiling <= 0 {
		p.Ceiling = 10 * time.Minute
	}
	if p.AdmissionGrace <= 0 {
		p.AdmissionGrace = 3 * time.Minute
	}
	if p.Interval <= 0 {
		p.Interval = 5 * time.Second
	}
	if p.StartupWindow <= 0 {
		p.StartupWindow = 24 * time.Hour
	}
	return p
}

// personaRecovery is the dispatcher's recovery state: only when each tenant was
// last swept. What is wrong with an invocation is read from the database on every
// sweep, never remembered, so any number of processes can run it.
type personaRecovery struct {
	mu   sync.Mutex
	last map[string]time.Time
	// unclean marks tenants whose last sweep left something it could not finish;
	// their next sweep looks as far back as a start does.
	unclean map[string]bool
	policy  personaRecoveryPolicy
}

// due reports whether the tenant is to be swept now, and how far back to look.
// The first sweep of a start reaches back over the whole startup window, so
// anything a previous process left behind is found.
func (r *personaRecovery) due(tenant string, now time.Time) (time.Duration, bool) {
	policy := r.policy.filled()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last == nil {
		r.last = make(map[string]time.Time)
	}
	previous, swept := r.last[tenant]
	if swept && now.Sub(previous) < policy.Interval {
		return 0, false
	}
	r.last[tenant] = now
	if !swept || r.unclean[tenant] {
		return policy.StartupWindow, true
	}
	return 2 * policy.Ceiling, true
}

// finished records how a sweep ended.
func (r *personaRecovery) finished(tenant string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.unclean == nil {
		r.unclean = make(map[string]bool)
	}
	r.unclean[tenant] = err != nil
}

// DispatchTenantRecovering finishes what a dead process left unfinished, then
// runs what is safe to run. It is the entry point of the background workload.
func (d *PersonaBackgroundDispatcher) DispatchTenantRecovering(ctx context.Context, tenant string, limit int) error {
	recoverErr := d.RecoverInterrupted(ctx, tenant)
	return errors.Join(recoverErr, d.DispatchTenant(ctx, tenant, limit))
}

// RecoverInterrupted sweeps one tenant's unfinished persona invocations and
// brings each to a final state or leaves it to the worker that may still run it:
//
//   - a claim that never reached admission, once older than the admission grace,
//     is finished as interrupted; a restarted server holds no person's authority,
//     so it cannot admit the question again, and the person asks again;
//   - an admitted run nobody holds (READY, or RUNNING with an expired lease) that
//     may already have spent a model call is finished as interrupted and never
//     run again on its own;
//   - such a run that has spent nothing and still has time is left for the
//     dispatcher, which runs it again once;
//   - a run past its deadline, or any run nobody holds past the ceiling, is
//     finished as interrupted whoever might still pick it up.
//
// Every decision is one conditional write: the run store compares the revision,
// the failure row is keyed by the post. Processes racing here finish an
// invocation once.
func (d *PersonaBackgroundDispatcher) RecoverInterrupted(ctx context.Context, tenant string) error {
	if d == nil || ctx == nil || d.worker == nil || d.worker.tenants == nil || d.agents == nil || d.tenantUUID == nil {
		return errPersonaRunModelWorker
	}
	cfg, err := d.worker.tenants.ForPersonaRunTenant(ctx, tenant)
	if err != nil || cfg.Now == nil || cfg.ExecutionStore == nil || cfg.AdmissionRecheck == nil {
		return errPersonaRunModelWorker
	}
	now := cfg.Now().UTC()
	window, due := d.recovery.due(tenant, now)
	if !due {
		return nil
	}
	invocations, err := d.recoveryInvocations()
	if err != nil {
		return err
	}
	items, err := invocations.ListUnfinished(ctx, tenant, now.Add(-window), 500)
	if err != nil {
		d.recovery.finished(tenant, err)
		return err
	}
	state, err := runstate.New(cfg.ExecutionStore, cfg.AdmissionRecheck)
	if err != nil {
		return err
	}
	policy := d.recovery.policy.filled()
	var failures []error
	for _, item := range items {
		if err := d.recoverOne(ctx, tenant, invocations, cfg, state, policy, item, now); err != nil {
			failures = append(failures, err)
		}
	}
	err = errors.Join(failures...)
	d.recovery.finished(tenant, err)
	return err
}

func (d *PersonaBackgroundDispatcher) recoveryInvocations() (*agentinvocationstore.Store, error) {
	return agentinvocationstore.NewWithTenantUUID(d.agents, func(tenant string) uuid.UUID { return d.tenantUUID(values.TenantId(tenant)) })
}

func (d *PersonaBackgroundDispatcher) recoverOne(ctx context.Context, tenant string, invocations *agentinvocationstore.Store, cfg PersonaRunStarterConfig, state *runstate.Service, policy personaRecoveryPolicy, item agentinvocationstore.UnfinishedInvocation, now time.Time) error {
	age := now.Sub(item.CreatedAt)
	finish := func(code string, retryable bool) error {
		err := invocations.FinishUnfinished(ctx, tenant, item, code, retryable)
		if errors.Is(err, agentinvocationstore.ErrConflict) {
			return nil // another process recorded the outcome first
		}
		if err == nil {
			slog.InfoContext(ctx, "hcmnext.persona_invocation_finished", "invocation_id", item.InvocationID, "code", code, "age_seconds", int64(age.Seconds()))
		}
		return err
	}
	switch {
	case item.RequestID == "":
		if age < policy.AdmissionGrace {
			return nil
		}
		return finish(runstate.InterruptedCode, true)
	case item.Decision != string(agentrun.DecisionAccepted):
		if age < policy.AdmissionGrace {
			return nil
		}
		return finish("ADMISSION_REFUSED", false)
	case item.RunState == "":
		if now.Before(item.Deadline) {
			return nil // admitted, with time left: the dispatcher creates and runs it
		}
		return finish(runstate.InterruptedCode, true)
	}
	run, err := cfg.ExecutionStore.Get(ctx, item.RequestID)
	if err != nil {
		if errors.Is(err, agentrunstate.ErrNotFound) {
			return nil
		}
		return err
	}
	switch run.State {
	case runstate.StateReady:
	case runstate.StateRunning:
		if run.Lease != nil && run.Lease.Until.After(now) {
			return nil // a worker holds it and renews its lease
		}
	default:
		// WAITING and RECONCILING belong to the run's own owner.
		return nil
	}
	if item.ModelCallStarted || !now.Before(run.Deadline) || age >= policy.Ceiling {
		return d.interruptRun(ctx, state, run, now, item.ModelCallStarted)
	}
	return nil
}

// interruptRun finishes an orphaned run as interrupted. Losing the race to
// another process, or finding the run already final, is success: the outcome the
// person sees is the same.
func (d *PersonaBackgroundDispatcher) interruptRun(ctx context.Context, state *runstate.Service, run runstate.Run, now time.Time, modelStarted bool) error {
	finished, err := state.InterruptOrphan(ctx, run.ID, run.Version, now)
	switch {
	case err == nil:
		slog.InfoContext(ctx, "hcmnext.persona_run_interrupted", "run_id", finished.ID, "model_call_started", modelStarted, "was", string(run.State))
		return nil
	case errors.Is(err, runstate.ErrConflict), errors.Is(err, agentrunstate.ErrConflict), errors.Is(err, runstate.ErrLease), errors.Is(err, runstate.ErrTerminal):
		return nil
	case errors.Is(err, runstate.ErrInvalid):
		// An unresolved effect: its owner reconciles it; recovery never hides it.
		slog.WarnContext(ctx, "hcmnext.persona_run_interrupt_refused", "run_id", run.ID)
		return nil
	}
	return err
}

// guardOrphanedRun runs before a restarted worker re-executes a run. It returns
// true when it finished the run itself, and the caller must stop. A run whose
// model call may have been paid for is never run again; one that spent nothing
// has its dead worker's open security steps closed so the fence admits the new
// worker.
func (d *PersonaBackgroundDispatcher) guardOrphanedRun(ctx context.Context, tenant string, state *runstate.Service, run runstate.Run, now time.Time) (bool, error) {
	if run.State != runstate.StateReady && !(run.State == runstate.StateRunning && (run.Lease == nil || !run.Lease.Until.After(now))) {
		return false, nil
	}
	tenantID := d.tenantUUID(values.TenantId(tenant))
	spent := false
	for _, checkpoint := range run.Checkpoints {
		spent = spent || checkpoint.Phase == runstate.PhaseModelCall
	}
	if !spent {
		started, err := d.agents.PersonaRunModelCallMayHaveStarted(ctx, tenantID, run.ID)
		if err != nil {
			return true, err // unknown is not safe: do not run, ask again next time
		}
		spent = started
	}
	if spent || !run.Deadline.After(now) {
		return true, d.interruptRun(ctx, state, run, now, spent)
	}
	_, err := d.agents.RecoverPersonaRunSteps(ctx, tenantID, run.ID, now)
	return false, err
}
