package timeclockstore

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

// EnsureSelfClockProjection creates revision 1 from durable session facts
// after workforce authority has resolved the worker and assignment.
func EnsureSelfClockProjection(ctx context.Context, store *timestore.Store, tenant, worker, assignment string) error {
	if store == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(worker) == "" || strings.TrimSpace(assignment) == "" {
		return timestore.ErrInvalid
	}
	return store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		return timestore.EnsureSelfClockProjection(ctx, tx, tenant, worker, assignment)
	})
}
