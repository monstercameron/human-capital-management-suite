package timeclockstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

// SessionAdapter maps clock session records to timestore session transactions.
type SessionAdapter struct{ Store *timestore.Store }

var _ clockservice.SessionStore = SessionAdapter{}

// OpenSession persists a new session and its opening event.
func (a SessionAdapter) OpenSession(ctx context.Context, tenant string, session clockservice.SessionRecord, event clockservice.SessionEvent) (clockservice.SessionRecord, error) {
	if a.Store == nil {
		return clockservice.SessionRecord{}, clockservice.ErrUnavailable
	}
	got, err := a.Store.OpenSession(ctx, tenant, sessionRow(session), eventRow(session.ID, event))
	return sessionRecord(got), err
}

// ApplyTransition persists a revision-checked session transition.
func (a SessionAdapter) ApplyTransition(ctx context.Context, tenant, sessionID string, expectedRevision uint64, next clockservice.SessionRecord, events []clockservice.SessionEvent) (clockservice.SessionRecord, error) {
	if a.Store == nil {
		return clockservice.SessionRecord{}, clockservice.ErrUnavailable
	}
	rows := make([]timestore.EventRow, len(events))
	for i, event := range events {
		rows[i] = eventRow(sessionID, event)
	}
	got, err := a.Store.ApplySessionTransition(ctx, tenant, sessionID, int64(expectedRevision), sessionRow(next), rows)
	return sessionRecord(got), err
}

// CurrentSession returns the open session for a worker assignment.
func (a SessionAdapter) CurrentSession(ctx context.Context, tenant, worker, assignment string) (clockservice.SessionRecord, error) {
	if a.Store == nil {
		return clockservice.SessionRecord{}, clockservice.ErrUnavailable
	}
	got, err := a.Store.CurrentSession(ctx, tenant, worker, assignment)
	return sessionRecord(got), err
}
