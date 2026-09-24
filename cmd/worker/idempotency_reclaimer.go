package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/platformidempotencystore"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

type idempotencyExpirer interface {
	ExpireTenant(string, time.Time) (int64, error)
}

// reclaimIdempotency visits only active tenants and delegates retention cutoff
// and compaction to the durable store's tenant-scoped operation.
func reclaimIdempotency(ctx context.Context, tenants tenantLister, expirer idempotencyExpirer, cutoff time.Time) (int64, error) {
	if tenants == nil || expirer == nil || cutoff.IsZero() {
		return 0, fmt.Errorf("worker: idempotency reclaimer is not configured")
	}
	ids, err := tenants.ActiveTenants(ctx)
	if err != nil {
		return 0, fmt.Errorf("worker: list tenants for idempotency reclamation: %w", err)
	}
	var total int64
	for _, tenant := range ids {
		if tenant == uuid.Nil {
			continue
		}
		count, err := expirer.ExpireTenant(tenant.String(), cutoff.UTC())
		if err != nil {
			return total, fmt.Errorf("worker: reclaim tenant %s idempotency: %w", tenant, err)
		}
		total += count
	}
	return total, nil
}

func runIdempotencyReclaimer(ctx context.Context, logger bootstrap.Logger, tenants tenantLister, expirer idempotencyExpirer, interval time.Duration, now func() time.Time) error {
	if interval < time.Minute {
		interval = time.Minute
	}
	clock := now
	if clock == nil {
		clock = time.Now
	}
	for {
		count, err := reclaimIdempotency(ctx, tenants, expirer, clock())
		if err != nil {
			logger.Error("worker.idempotency_reclamation_failed", "error", err.Error())
		} else if count > 0 {
			logger.Info("worker.idempotency_reclaimed", "rows", count)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func idempotencyReclaimerFor(pool workerPool) idempotencyExpirer {
	return platformidempotencystore.New(pool)
}
