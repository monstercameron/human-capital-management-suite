package timeclockstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
)

var _ clockservice.PunchObservationLookup = ObservationAdapter{}

// LookupPunchObservation reads one immutable observation by the durable
// tenant/source/idempotency key. It is used before the workflow start so a
// replay does not scan a bounded occurrence-time window.
func (a ObservationAdapter) LookupPunchObservation(ctx context.Context, tenant, source, idempotencyKey string) (clockservice.ObservationRecord, bool, error) {
	if a.Store == nil {
		return clockservice.ObservationRecord{}, false, clockservice.ErrUnavailable
	}
	row, found, err := a.Store.LookupPunchObservation(ctx, tenant, source, idempotencyKey)
	if err != nil || !found {
		return clockservice.ObservationRecord{}, found, err
	}
	return punchObservationRecord(row), true, nil
}
