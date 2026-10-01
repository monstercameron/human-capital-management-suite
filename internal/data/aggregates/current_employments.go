package aggregates

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ActiveEmploymentsForWorker returns every current employment row for one
// worker. The full result is intentional: callers resolving owner facts must
// detect ambiguous concurrent employments rather than accepting QueryRow's
// arbitrary first result.
func (PeopleStore) ActiveEmploymentsForWorker(ctx context.Context, ex Executor, tenant, workerID uuid.UUID, businessAt time.Time) ([]Employment, error) {
	if ctx == nil || ex == nil || tenant == uuid.Nil || workerID == uuid.Nil || businessAt.IsZero() {
		return nil, fmt.Errorf("aggregates: tenant, worker and business instant are required")
	}
	rows, err := ex.Query(ctx, currentByRefSQL("employment", employmentColumns, "worker_ref")+` ORDER BY entity_id`, tenant, businessAt, workerID)
	if err != nil {
		return nil, fmt.Errorf("aggregates: read current employments for worker %s: %w", workerID, err)
	}
	defer rows.Close()
	out := make([]Employment, 0, 1)
	for rows.Next() {
		item, scanErr := scanEmployment(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("aggregates: scan current employment for worker %s: %w", workerID, scanErr)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("aggregates: iterate current employments for worker %s: %w", workerID, err)
	}
	return out, nil
}
