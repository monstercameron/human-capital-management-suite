package timeclockstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
)

// ListEvents exposes timestore's committed clock outbox through the
// clockservice port used by the workflow dispatcher. It is read-only; event
// delivery remains outside the punch transaction and is recoverable by replay.
func (a Adapter) ListEvents(ctx context.Context, tenant string, afterCursor int64, limit int) ([]clockservice.OutboxEvent, error) {
	if a.Store == nil {
		return nil, clockservice.ErrUnavailable
	}
	events, err := a.Store.ListEvents(ctx, tenant, afterCursor, limit)
	if err != nil {
		return nil, err
	}
	out := make([]clockservice.OutboxEvent, len(events))
	for i, event := range events {
		out[i] = clockservice.OutboxEvent{Sequence: event.Sequence, EventType: event.EventType, SchemaVersion: event.SchemaVersion, Payload: append([]byte(nil), event.Payload...), CreatedAt: event.CreatedAt}
	}
	return out, nil
}

var _ clockservice.WorkflowEventSource = Adapter{}
