package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
)

const (
	// A private agent answer is hidden from its reader the moment it expires;
	// the sweep only reclaims the row, so a quarter of an hour is soon enough.
	agentEphemeralPruneInterval = 15 * time.Minute
	// agentEphemeralPruneBatch is one bounded deletion (the store refuses more
	// than a thousand rows in one statement).
	agentEphemeralPruneBatch = 500
	// agentEphemeralPruneRounds bounds one tenant's share of one sweep, so a
	// large backlog in one tenant does not hold the others back.
	agentEphemeralPruneRounds = 20
)

// agentEphemeralPruner deletes a bounded batch of private agent answers whose
// view lifetime has ended.
type agentEphemeralPruner interface {
	PruneExpiredEphemeral(ctx context.Context, tenantID string, before time.Time, limit int) (int64, error)
}

// pruneExpiredAgentEphemeralPosts removes, for one tenant, the private answers
// that expired at or before now. It deletes in bounded batches and stops when
// a batch comes back short or the tenant's share of the sweep is used.
func pruneExpiredAgentEphemeralPosts(ctx context.Context, pruner agentEphemeralPruner, tenant string, now time.Time) (int64, error) {
	var removed int64
	for range agentEphemeralPruneRounds {
		batch, err := pruner.PruneExpiredEphemeral(ctx, tenant, now, agentEphemeralPruneBatch)
		removed += batch
		if err != nil {
			return removed, err
		}
		if batch < agentEphemeralPruneBatch {
			break
		}
	}
	return removed, nil
}

// agentEphemeralPruneWorkload is the served sweep of expired private agent
// answers. Reads already hide an expired answer; without this sweep its text
// stayed in the database for good. It runs beside the other chat background
// work, for the tenants this process serves.
func agentEphemeralPruneWorkload(store *chatstore.Store, tenants []string, now func() time.Time, logger personaInvocationLogger) *bootstrap.Workload {
	if store == nil || now == nil {
		return nil
	}
	return agentEphemeralPruneWorkloadFor(chatstore.NewDurableEphemeralStore(store), tenants, now, logger, agentEphemeralPruneInterval)
}

func agentEphemeralPruneWorkloadFor(pruner agentEphemeralPruner, tenants []string, now func() time.Time, logger personaInvocationLogger, interval time.Duration) *bootstrap.Workload {
	if isNilPersonaOutputPort(pruner) || now == nil || interval <= 0 {
		return nil
	}
	scoped := make([]string, 0, len(tenants))
	seen := make(map[string]bool, len(tenants))
	for _, tenant := range tenants {
		if strings.TrimSpace(tenant) != tenant || values.TenantId(tenant).Validate() != nil {
			return nil
		}
		if !seen[tenant] {
			seen[tenant] = true
			scoped = append(scoped, tenant)
		}
	}
	if len(scoped) == 0 {
		return nil
	}
	return &bootstrap.Workload{Name: "chat-ephemeral-prune", Run: func(ctx context.Context) error {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			for _, tenant := range scoped {
				if ctx.Err() != nil {
					return nil
				}
				// One tenant's failure is logged and the next tenant is still swept;
				// the rows stay hidden from readers either way.
				if _, err := pruneExpiredAgentEphemeralPosts(ctx, pruner, tenant, now().UTC()); err != nil && ctx.Err() == nil && !isNilPersonaOutputPort(logger) {
					logger.Error("hcmnext.chat_ephemeral_prune_failed", "tenant_id", tenant, "error_type", fmt.Sprintf("%T", err))
				}
			}
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	}}
}
