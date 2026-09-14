package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	hcmotel "github.com/monstercameron/human-capital-management-suite/internal/platform/telemetry/otel"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/progress"
)

const (
	// ComponentWorkloadProgress is WF-RUN-020's stuck-workflow sweep.
	ComponentWorkloadProgress = "workload:workflow-progress"
	workloadNameProgress      = "workflow-progress"
	// progressSweepInterval is how often the sweep looks for missed
	// expectations. It is far shorter than any expectation's grace, so a
	// stuck instance is raised within one interval of its grace elapsing.
	progressSweepInterval = time.Minute
)

// progressRoute is the authoritative owner of stuck-workflow incidents.
func progressRoute() progress.Route {
	return progress.Route{
		PrimaryOwner:   "team:workflow-runtime",
		SecondaryRoute: "team:platform-oncall",
		StormLimit:     25,
		StormWindow:    time.Hour,
	}
}

// composeProgressWorkload builds the periodic stuck-workflow sweep over the
// serve tenant. It runs beside the timer scheduler: the scheduler moves
// work, and this workload notices when something it was supposed to move has
// not moved.
func composeProgressWorkload(cfg ServeConfig, pool *pgxadapter.Pool, provider *hcmotel.Provider, logger bootstrap.Logger, now func() time.Time) (bootstrap.Workload, *progress.Sweeper, error) {
	if pool == nil {
		return bootstrap.Workload{}, nil, fmt.Errorf("application: the workflow progress sweep needs a database pool")
	}
	slogger, _ := logger.(*slog.Logger)
	if slogger == nil {
		slogger = slog.Default()
	}
	sweeper := &progress.Sweeper{
		Begin:    pool.Begin,
		Policy:   progress.DefaultPolicy(),
		Route:    progressRoute(),
		Observer: execution.NewProgressObserver(provider, slogger, now),
		Clock:    now,
	}
	tenant := pgstore.TenantID(cfg.Tenant)
	return bootstrap.Workload{Name: workloadNameProgress, Run: func(ctx context.Context) error {
		return runProgressSweeps(ctx, func(ctx context.Context) error {
			_, err := sweeper.Sweep(ctx, tenant)
			return err
		}, progressSweepInterval)
	}}, sweeper, nil
}

// runProgressSweeps sweeps immediately and then every interval until ctx is
// done. A failed sweep is reported through the sweeper's observer and never
// stops the workload: a detector that dies on its first database hiccup
// would leave stuck workflows unwatched.
func runProgressSweeps(ctx context.Context, sweep func(context.Context) error, interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("application: progress sweep interval %s must be positive", interval)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		_ = sweep(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
