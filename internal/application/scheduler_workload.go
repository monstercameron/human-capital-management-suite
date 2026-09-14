package application

import (
	"context"
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

func timerDispatcher(tenantID string, tenant string, resume firedTimerResumer) executionscheduler.Dispatcher {
	return executionscheduler.DispatcherFunc(func(ctx context.Context, work executionscheduler.Work) (executionscheduler.Disposition, error) {
		if work.Row.TenantID.String() != tenantID {
			return executionscheduler.DispositionAbandoned, nil
		}
		result, err := resume(app.WithResumeTenant(ctx, tenant), work.Row.InstanceID.String(), work.Row.NodeID, work.Row.Attempt)
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
	runner, err := executionscheduler.New(executionscheduler.Config{
		DB: pool,
		Claims: []lease.AcquireRequest{{
			TenantID: claimTenant,
			Resource: lease.Resource{Kind: lease.ResourceQueue, ID: executionscheduler.DefaultQueueKey},
			Holder:   lease.Identity{WorkloadRef: "workload:hcmnext-serve", InstanceRef: identity},
		}},
		Leases: lease.Manager{}, Timers: timer.Scheduler{Attempts: runtime.Store{}},
		Misfire:    schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, Grace: time.Hour, MaxCatchUp: 1},
		Dispatcher: dispatcher, Logger: logger, Clock: now,
		Recorder: schedulerRecorder(provider, logger, now),
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
