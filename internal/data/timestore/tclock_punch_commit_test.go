package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func humanPunchEntry(id, digest string) AtomicBatchEntry {
	at := time.Unix(200, 0).UTC()
	payload := json.RawMessage(`{"session_id":"session-human","observation_id":"obs-human"}`)
	return AtomicBatchEntry{
		Receipt:      ReceiptRow{ObservationID: id},
		SessionIsNew: true,
		Session:      &SessionRow{ID: "session-human", TenantID: "tenant-human", WorkerRef: "worker-human", AssignmentRef: "assignment-human", Status: "OPEN", Source: "WORKER_SELF", OpenedAt: at, Payload: payload},
		Event:        &EventRow{SessionID: "session-human", Kind: "OPENED", ActorRef: "worker-human", IdempotencyKey: "event-" + id, Digest: digest, Payload: json.RawMessage(`{"observation_id":"obs-human"}`)},
		Observation:  &ObservationRow{ID: id, TenantID: "tenant-human", WorkerRef: "worker-human", AssignmentRef: "assignment-human", Source: "WORKER_SELF", EventType: "IN", ProjectRef: "project-human", Timezone: "UTC", IdempotencyKey: "punch-human", Digest: digest, OccurredAt: at, ReceivedAt: at.Add(time.Second), Payload: json.RawMessage(`{"kind":"human"}`)},
	}
}

func TestTodo_WTIME004_CommitPunchEffectAtomicReplayAndLoad(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	entry := humanPunchEntry("obs-human", "digest-human")
	if err := s.RunTenantTx(ctx, "tenant-human", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO time_self_clock_projection(tenant_id,worker_ref,assignment_ref) VALUES($1,$2,$3)`, "tenant-human", "worker-human", "assignment-human")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	first, err := s.CommitPunchEffect(ctx, "tenant-human", entry)
	if err != nil || first.Duplicate || first.Observation.ID != "obs-human" || first.Session.ID != "session-human" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := s.CommitPunchEffect(ctx, "tenant-human", entry)
	if err != nil || !replay.Duplicate {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	events, err := s.ListEvents(ctx, "tenant-human", 0, 20)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
	loaded, found, err := s.LoadPunchEffect(ctx, "tenant-human", "obs-human")
	if err != nil || !found || loaded.Session.ID != "session-human" || loaded.Observation.ID != "obs-human" {
		t.Fatalf("loaded=%+v found=%v err=%v", loaded, found, err)
	}
	changed := humanPunchEntry("obs-human", "changed")
	if _, err := s.CommitPunchEffect(ctx, "tenant-human", changed); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed replay err=%v", err)
	}
}

func TestTodo_WTIME004_CommitPunchEffectRollsBackAllRows(t *testing.T) {
	s := fixture(t)
	if err := s.RunTenantTx(context.Background(), "tenant-human", func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO time_self_clock_projection(tenant_id,worker_ref,assignment_ref) VALUES($1,$2,$3)`, "tenant-human", "worker-human", "assignment-human")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	entry := humanPunchEntry("obs-rollback", "digest-rollback")
	entry.Observation.Payload = json.RawMessage("{")
	if _, err := s.CommitPunchEffect(context.Background(), "tenant-human", entry); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err=%v", err)
	}
	if _, err := s.CurrentSession(context.Background(), "tenant-human", "worker-human", "assignment-human"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session survived rollback: %v", err)
	}
	events, err := s.ListEvents(context.Background(), "tenant-human", 0, 20)
	if err != nil || len(events) != 0 {
		t.Fatalf("outbox survived rollback: %d %v", len(events), err)
	}
}

func TestTodo_WTIME004_ProjectionCASConcurrentWebPunches(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if err := s.RunTenantTx(ctx, "tenant-human", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO time_self_clock_projection(tenant_id,worker_ref,assignment_ref) VALUES($1,$2,$3)`, "tenant-human", "worker-human", "assignment-human")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	entries := []AtomicBatchEntry{humanPunchEntry("obs-web-a", "digest-web-a"), humanPunchEntry("obs-web-b", "digest-web-b")}
	for i := range entries {
		entries[i].Observation.Source = "WEB"
		entries[i].Observation.ID = "obs-web-" + string(rune('a'+i))
		entries[i].Observation.IdempotencyKey = "key-web-" + string(rune('a'+i))
		entries[i].Session.ID = "session-web-" + string(rune('a'+i))
		entries[i].Event.SessionID = entries[i].Session.ID
		entries[i].Event.IdempotencyKey = "event-web-" + string(rune('a'+i))
		entries[i].ExpectedProjectionRevision = 1
	}
	type result struct{ err error }
	results := make(chan result, len(entries))
	var wg sync.WaitGroup
	for _, entry := range entries {
		wg.Add(1)
		go func(entry AtomicBatchEntry) {
			defer wg.Done()
			_, err := s.CommitPunchEffect(ctx, "tenant-human", entry)
			results <- result{err: err}
		}(entry)
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for r := range results {
		if r.err == nil {
			wins++
		} else if errors.Is(r.err, ErrRevisionConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent error: %v", r.err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
	var revision int64
	if err := s.RunTenantTx(ctx, "tenant-human", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT revision FROM time_self_clock_projection WHERE tenant_id=$1 AND worker_ref=$2 AND assignment_ref=$3`, "tenant-human", "worker-human", "assignment-human").Scan(&revision)
	}); err != nil || revision != 2 {
		t.Fatalf("revision=%d err=%v", revision, err)
	}
	sessions, _, err := s.ListSessions(ctx, "tenant-human", "worker-human", time.Time{}, time.Time{}, "", 10)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions=%d err=%v", len(sessions), err)
	}
	current := sessions[0]
	out := AtomicBatchEntry{
		ExpectedProjectionRevision: 2,
		ExpectedRevision:           current.Revision,
		Session:                    &SessionRow{ID: current.ID, TenantID: current.TenantID, WorkerRef: current.WorkerRef, AssignmentRef: current.AssignmentRef, Status: "CLOSED", Source: "WEB", ProjectRef: current.ProjectRef, Revision: current.Revision, OpenedAt: current.OpenedAt, ClosedAt: time.Unix(400, 0).UTC(), Payload: current.Payload},
		Event:                      &EventRow{SessionID: current.ID, Kind: "CLOSED", ActorRef: "worker-human", IdempotencyKey: "event-web-out", Digest: "digest-web-out", Payload: json.RawMessage(`{"observation_id":"obs-web-out"}`)},
		Observation:                &ObservationRow{ID: "obs-web-out", TenantID: "tenant-human", WorkerRef: "worker-human", AssignmentRef: "assignment-human", Source: "WEB", EventType: "OUT", ProjectRef: current.ProjectRef, Timezone: "UTC", IdempotencyKey: "key-web-out", Digest: "digest-web-out", OccurredAt: time.Unix(400, 0).UTC(), ReceivedAt: time.Unix(401, 0).UTC(), Payload: json.RawMessage(`{"kind":"out"}`)},
	}
	if _, err := s.CommitPunchEffect(ctx, "tenant-human", out); err != nil {
		t.Fatalf("out err=%v", err)
	}
	if _, err := s.CommitPunchEffect(ctx, "tenant-human", out); err != nil {
		t.Fatalf("out replay err=%v", err)
	}
	if err := s.RunTenantTx(ctx, "tenant-human", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT revision FROM time_self_clock_projection WHERE tenant_id=$1 AND worker_ref=$2 AND assignment_ref=$3`, "tenant-human", "worker-human", "assignment-human").Scan(&revision)
	}); err != nil || revision != 3 {
		t.Fatalf("out replay revision=%d err=%v", revision, err)
	}
}
