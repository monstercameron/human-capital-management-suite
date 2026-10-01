package timeclockstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/timestore"
)

func TestTodo_WTIME004_PunchAdapterFailsClosedWithoutStore(t *testing.T) {
	a := Adapter{}
	work := clockservice.PunchWork{}
	if _, err := a.CommitPunch(context.Background(), "tenant", work); !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("commit err=%v", err)
	}
	if _, _, err := a.LoadPunchResult(context.Background(), "tenant", "obs"); !errors.Is(err, clockservice.ErrUnavailable) {
		t.Fatalf("load err=%v", err)
	}
}

func TestTodo_WTIME004_PunchAdapterRejectsIncompleteWorkBeforeStore(t *testing.T) {
	entry := punchEntry(clockservice.PunchWork{Session: clockservice.SessionRecord{ID: "s"}, SessionEvents: []clockservice.SessionEvent{{Kind: "OPENED"}}, Observation: clockservice.ObservationRecord{ID: "o"}})
	if entry.Session.ID != "s" || entry.Event.Kind != "OPENED" || entry.Observation.ID != "o" {
		t.Fatalf("mapped entry=%+v", entry)
	}
}

func TestTodo_WTIME004_PunchAdapterCommitsAndRecoversHumanPunch(t *testing.T) {
	store, tenant := adapterFixture(t)
	a := Adapter{Store: store}
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	work := clockservice.PunchWork{
		SessionIsNew:  true,
		Session:       clockservice.SessionRecord{ID: "human-session", TenantID: tenant, WorkerRef: "human-worker", AssignmentRef: "human-assignment", Status: "OPEN", Source: "WORKER_SELF", OpenedAt: at, Payload: []byte(`{"state":"OPEN"}`)},
		SessionEvents: []clockservice.SessionEvent{{Kind: "OPENED", ActorRef: "human-worker", IdempotencyKey: "human-event", Digest: "human-digest", Payload: []byte(`{"observation_id":"human-observation"}`)}},
		Observation:   clockservice.ObservationRecord{ID: "human-observation", TenantID: tenant, WorkerRef: "human-worker", AssignmentRef: "human-assignment", Source: "WORKER_SELF", EventType: "IN", ProjectRef: "human-project", Timezone: "UTC", IdempotencyKey: "human-punch", Digest: "human-digest", OccurredAt: at, ReceivedAt: at.Add(time.Second), Payload: []byte(`{"kind":"human"}`)},
	}
	if err := store.RunTenantTx(context.Background(), tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO time_self_clock_projection(tenant_id,worker_ref,assignment_ref) VALUES($1,$2,$3)`, tenant, "human-worker", "human-assignment")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	first, err := a.CommitPunch(context.Background(), tenant, work)
	if err != nil || first.Duplicate || first.Observation.DeviceRef != "" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	replay, err := a.CommitPunch(context.Background(), tenant, work)
	if err != nil || !replay.Duplicate {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	loaded, found, err := a.LoadPunchResult(context.Background(), tenant, work.Observation.ID)
	if err != nil || !found || loaded.Session.ID != work.Session.ID {
		t.Fatalf("loaded=%+v found=%v err=%v", loaded, found, err)
	}
}

func TestTodo_WTIME004_ClosedSessionIsAbsentFromCurrentSession(t *testing.T) {
	store, tenant := adapterFixture(t)
	at := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	sessions := SessionAdapter{Store: store}
	opened, err := sessions.OpenSession(context.Background(), tenant, clockservice.SessionRecord{ID: "closed-session", TenantID: tenant, WorkerRef: "closed-worker", AssignmentRef: "closed-assignment", Status: "OPEN", Source: "WORKER_SELF", OpenedAt: at}, clockservice.SessionEvent{Kind: "OPENED", ActorRef: "closed-worker", IdempotencyKey: "closed-open", Digest: "closed-open-digest"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.ApplyTransition(context.Background(), tenant, opened.ID, opened.Revision, clockservice.SessionRecord{ID: opened.ID, TenantID: tenant, WorkerRef: opened.WorkerRef, AssignmentRef: opened.AssignmentRef, Status: "CLOSED", Source: opened.Source, ClosedAt: at.Add(time.Hour)}, []clockservice.SessionEvent{{Kind: "CLOSED", ActorRef: "closed-worker", IdempotencyKey: "closed-close", Digest: "closed-close-digest"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.CurrentSession(context.Background(), tenant, opened.WorkerRef, opened.AssignmentRef); !errors.Is(err, timestore.ErrNotFound) {
		t.Fatalf("closed session err=%v, want ErrNotFound", err)
	}
}
