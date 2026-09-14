package progress

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

// Observer is the sweep's telemetry port. This package never opens an
// OpenTelemetry span or writes a log record itself (LIB-007 admits only
// internal/platform/telemetry/otel to import OTel); the composition supplies
// an implementation, and [NoopObserver] is the default.
type Observer interface {
	// StartSweep brackets one tenant sweep. The returned end func receives the
	// sweep's result and error.
	StartSweep(ctx context.Context, tenant string) (context.Context, func(SweepResult, error))
	// Stuck reports one stuck instance with its findings and whether its
	// incident was newly opened (true) or linked to one already open (false).
	Stuck(ctx context.Context, s Snapshot, findings []Finding, incidentKey string, opened bool)
}

// NoopObserver records nothing.
type NoopObserver struct{}

// StartSweep implements Observer.
func (NoopObserver) StartSweep(ctx context.Context, _ string) (context.Context, func(SweepResult, error)) {
	return ctx, func(SweepResult, error) {}
}

// Stuck implements Observer.
func (NoopObserver) Stuck(context.Context, Snapshot, []Finding, string, bool) {}

// SweepResult summarizes one tenant sweep.
type SweepResult struct {
	Instances int
	Stuck     int
	Opened    int
	Linked    int
	Failed    int
}

// Sweeper reads one tenant's live instances, detects missed expectations and
// raises one incident per stuck condition. Every read and write runs in a
// transaction bound to the tenant through tenancy.WithTenant, so row-level
// security confines a sweep to its own tenant even under the application role.
type Sweeper struct {
	Begin    func(ctx context.Context) (dbport.Tx, error)
	Policy   Policy
	Route    Route
	Limit    int
	Observer Observer
	Clock    func() time.Time
}

// Sweep runs one sweep for tenant. Each stuck instance's incident is raised in
// its own transaction, so one conflicting or storm-refused incident does not
// hide the others; the first such error is returned after every instance has
// been attempted.
func (w Sweeper) Sweep(ctx context.Context, tenant uuid.UUID) (SweepResult, error) {
	obs := w.Observer
	if obs == nil {
		obs = NoopObserver{}
	}
	ctx, end := obs.StartSweep(ctx, tenant.String())
	result, err := w.sweep(ctx, obs, tenant)
	end(result, err)
	return result, err
}

func (w Sweeper) sweep(ctx context.Context, obs Observer, tenant uuid.UUID) (SweepResult, error) {
	if w.Begin == nil {
		return SweepResult{}, fmt.Errorf("progress: sweeper needs a transaction opener")
	}
	if tenant == uuid.Nil {
		return SweepResult{}, fmt.Errorf("progress: tenant is required")
	}
	clock := w.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	limit := w.Limit
	if limit == 0 {
		limit = 1000
	}
	snaps, err := w.load(ctx, tenant, limit)
	if err != nil {
		return SweepResult{}, err
	}
	now := clock()
	result := SweepResult{Instances: len(snaps)}
	var firstErr error
	for _, s := range snaps {
		findings, err := Detect(s, w.Policy, now)
		if err != nil {
			return result, err
		}
		if len(findings) == 0 {
			continue
		}
		result.Stuck++
		opened, err := w.raise(ctx, tenant, s, findings)
		if err != nil {
			result.Failed++
			if firstErr == nil {
				firstErr = fmt.Errorf("progress: raise incident for instance %s: %w", s.Instance.InstanceID, err)
			}
			continue
		}
		if opened {
			result.Opened++
		} else {
			result.Linked++
		}
		obs.Stuck(ctx, s, findings, IncidentKey(s.Instance.InstanceID, findings), opened)
	}
	return result, firstErr
}

func (w Sweeper) load(ctx context.Context, tenant uuid.UUID, limit int) ([]Snapshot, error) {
	tx, err := w.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("progress: begin read: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return nil, fmt.Errorf("progress: scope read to tenant: %w", err)
	}
	return LoadSnapshots(ctx, tx, tenant, limit)
}

func (w Sweeper) raise(ctx context.Context, tenant uuid.UUID, s Snapshot, findings []Finding) (bool, error) {
	tx, err := w.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		return false, err
	}
	res, err := RaiseIncident(ctx, tx, tenant, s, findings, w.Route)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return res.Created, nil
}
