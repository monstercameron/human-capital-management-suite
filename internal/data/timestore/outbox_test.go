package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTodo_TCLOCK_012_Integration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenantA, tenantB := "tenant-a", "tenant-b"
	opened := time.Now().UTC().Truncate(time.Second)

	// Three committed state changes for tenant A: an open, a punch, and a
	// second open for a different assignment.
	sessionA, eventA := newOpenSession(uuid.NewString(), tenantA, "worker-1", "assignment-1", opened)
	if _, err := s.OpenSession(ctx, tenantA, sessionA, eventA); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	obs := ObservationRow{
		ID: uuid.NewString(), TenantID: tenantA, WorkerRef: "worker-1", AssignmentRef: "assignment-1",
		Source: "KIOSK", EventType: "BREAK_START", OccurredAt: opened, ReceivedAt: opened,
		IdempotencyKey: "punch:break", Digest: "sha256:break", Payload: json.RawMessage(`{}`),
	}
	if _, _, err := s.AppendObservation(ctx, tenantA, obs); err != nil {
		t.Fatalf("AppendObservation: %v", err)
	}
	sessionA2, eventA2 := newOpenSession(uuid.NewString(), tenantA, "worker-2", "assignment-2", opened)
	if _, err := s.OpenSession(ctx, tenantA, sessionA2, eventA2); err != nil {
		t.Fatalf("second OpenSession: %v", err)
	}

	// A separate tenant's events must never appear on tenant A's cursor.
	sessionB, eventB := newOpenSession(uuid.NewString(), tenantB, "worker-1", "assignment-1", opened)
	if _, err := s.OpenSession(ctx, tenantB, sessionB, eventB); err != nil {
		t.Fatalf("tenant B OpenSession: %v", err)
	}

	page1, err := s.ListEvents(ctx, tenantA, 0, 2)
	if err != nil || len(page1) != 2 {
		t.Fatalf("page1 = %d, %v", len(page1), err)
	}
	if page1[0].Sequence >= page1[1].Sequence {
		t.Fatalf("events are not in increasing sequence order: %+v", page1)
	}
	page2, err := s.ListEvents(ctx, tenantA, page1[len(page1)-1].Sequence, 2)
	if err != nil || len(page2) != 1 {
		t.Fatalf("page2 = %d, %v", len(page2), err)
	}
	for _, e := range append(page1, page2...) {
		if e.TenantID != tenantA {
			t.Fatalf("leaked event from another tenant: %+v", e)
		}
	}

	// Re-reading from the same cursor is stable and does not consume rows
	// (a crashed subscriber can always resume from its last acked cursor).
	replay, err := s.ListEvents(ctx, tenantA, 0, 2)
	if err != nil || len(replay) != 2 || replay[0].Sequence != page1[0].Sequence {
		t.Fatalf("replay from cursor 0 = %+v, %v", replay, err)
	}

	// An event is never visible before its transaction commits: a failed
	// AppendObservation (duplicate lineage target) must not leave a
	// partial outbox row behind.
	before, err := s.ListEvents(ctx, tenantA, 0, 100)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	bogus := obs
	bogus.ID = uuid.NewString()
	bogus.CorrectsID = uuid.NewString()
	bogus.IdempotencyKey = "punch:bogus-correction"
	bogus.Digest = "sha256:bogus-correction"
	if _, _, err := s.AppendObservation(ctx, tenantA, bogus); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected rejection: %v", err)
	}
	after, err := s.ListEvents(ctx, tenantA, 0, 100)
	if err != nil || len(after) != len(before) {
		t.Fatalf("outbox grew after a rejected observation: before=%d after=%d", len(before), len(after))
	}

	// Input validation.
	if _, err := s.ListEvents(ctx, tenantA, -1, 10); !errors.Is(err, ErrInvalid) {
		t.Fatalf("negative cursor: %v", err)
	}
	if _, err := s.ListEvents(ctx, "", 0, 10); !errors.Is(err, ErrInvalid) {
		t.Fatalf("blank tenant: %v", err)
	}
}
