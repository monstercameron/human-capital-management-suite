package application

import (
	"context"
	"errors"
	"fmt"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"log/slog"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	executionscheduler "github.com/monstercameron/human-capital-management-suite/internal/platform/execution/scheduler"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

const (
	ComponentWorkloadScheduler = "workload:workflow-scheduler"
	workloadNameScheduler      = "workflow-scheduler"
)

func timerDispatchDisposition(result app.ExecutionResult, err error) executionscheduler.Disposition {
	if err != nil {
		return executionscheduler.DispositionRetry
	}
	if result.InstanceID == "" {
		return executionscheduler.DispositionAbandoned
	}
	return executionscheduler.DispositionCompleted
}

type firedTimerResumer func(context.Context, string, string, int) (app.ExecutionResult, error)

// timerDispatcher adapts the cell's fired-timer resume to the scheduler. A
// resume whose WAIT node is no longer on the instance frontier
// ([app.ErrParkedResumeStale]) settles COMPLETED with no error, exactly as
// [signalResumer] does: the timer's wake was already consumed by an earlier
// delivery of this row, and the scheduler treats any returned error as RETRY,
// so reporting it would put the row back to READY and redispatch it on every
// tick forever.
func timerDispatcher(tenantID string, tenant string, resume firedTimerResumer) executionscheduler.Dispatcher {
	return executionscheduler.DispatcherFunc(func(ctx context.Context, work executionscheduler.Work) (executionscheduler.Disposition, error) {
		if work.Row.TenantID.String() != tenantID {
			return executionscheduler.DispositionAbandoned, nil
		}
		result, err := resume(app.WithResumeTenant(ctx, tenant), work.Row.InstanceID.String(), work.Row.NodeID, work.Row.Attempt)
		if errors.Is(err, app.ErrParkedResumeStale) {
			return executionscheduler.DispositionCompleted, nil
		}
		return timerDispatchDisposition(result, err), err
	})
}

func composeSchedulerWorkload(cfg ServeConfig, pool *pgxadapter.Pool, identity string, cell *app.Cell, provider *hcmotel.Provider, logger bootstrap.Logger, now func() time.Time) (bootstrap.Workload, error) {
	if pool == nil {
		return bootstrap.Workload{}, fmt.Errorf("application: -%s needs a database pool", FieldScheduler)
	}
	// The serve composition uses the same deterministic tenant mapper as the
	// store and execution authority.
	claimTenant := pgstore.TenantID(cfg.Tenant)
	dispatcher := timerDispatcher(claimTenant.String(), cfg.Tenant,
		func(ctx context.Context, instanceID, nodeID string, attempt int) (app.ExecutionResult, error) {
			return cell.ResumeFiredTimer(ctx, instanceID, nodeID, attempt)
		})
	// WF-RUN-005: the signal role claims matched signal continuations under
	// the queue fence, and the generic dispatcher routes any it reaches first.
	signals, err := executionscheduler.NewSignalDispatcher(executionscheduler.SignalDispatcherConfig{
		DB: pool, Resumer: signalResumer(claimTenant.String(), cfg.Tenant, cell.ResumeMatchedSignal),
		Expirer: signalExpirer(claimTenant.String(), cfg.Tenant, cell.ResumeExpiredSignal),
		Clock:   now, Logger: logger,
	})
	if err != nil {
		return bootstrap.Workload{}, err
	}
	// WF-RUN-003: the same replica sweeps for instances a dead driver left
	// READY with no ready work, and redelivers each under a lease takeover.
	recovery, err := composeRecoveryRole(pool, identity, claimTenant.String(), cfg.Tenant,
		func(ctx context.Context, instanceID string, expectedVersion int64) (app.ExecutionResult, error) {
			return cell.RedeliverReady(ctx, instanceID, expectedVersion)
		})
	if err != nil {
		return bootstrap.Workload{}, err
	}
	runner, err := executionscheduler.New(executionscheduler.Config{
		DB: pool,
		Claims: []lease.AcquireRequest{{
			TenantID: claimTenant,
			Resource: lease.Resource{Kind: lease.ResourceQueue, ID: executionscheduler.DefaultQueueKey},
			Holder:   lease.Identity{WorkloadRef: "workload:hcmnext-serve", InstanceRef: identity},
		}},
		Leases: lease.Manager{}, Timers: timer.Scheduler{Attempts: runtime.Store{}},
		Misfire:    schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, Grace: time.Hour, MaxCatchUp: 1},
		Dispatcher: signals.Route(dispatcher), Logger: logger, Clock: now,
		Recorder: schedulerRecorder(provider, logger, now), SignalRole: signals,
		RecoveryRole: recovery,
	})
	if err != nil {
		return bootstrap.Workload{}, err
	}
	return bootstrap.Workload{Name: workloadNameScheduler, Run: func(ctx context.Context) error {
		return runner.Run(ctx, executionscheduler.DefaultPollInterval)
	}}, nil
}

// schedulerRecorder is the workflow-engine telemetry recorder for the timer
// scheduler: every claim it serves and every lease, timer and runtime
// operation beneath it becomes a span and a structured log line. Without a
// telemetry provider it still logs refusals and failures.
func schedulerRecorder(provider *hcmotel.Provider, logger bootstrap.Logger, now func() time.Time) observe.Recorder {
	slogger, _ := logger.(*slog.Logger)
	return execution.NewObserveRecorder(provider, slogger, now)
}
