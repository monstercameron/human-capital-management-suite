package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

func TestTodo_TCLOCK_003_AtomicBatchRejectsInvalidEnvelope(t *testing.T) {
	s := &Store{}
	for name, tc := range map[string]struct {
		tenant, device string
		entries        []AtomicBatchEntry
	}{
		"missing tenant": {device: "device", entries: []AtomicBatchEntry{{Sequence: 1, Receipt: ReceiptRow{DeviceSequence: 1, Status: "REJECTED"}}}},
		"missing device": {tenant: "tenant", entries: []AtomicBatchEntry{{Sequence: 1, Receipt: ReceiptRow{DeviceSequence: 1, Status: "REJECTED"}}}},
		"missing rows":   {tenant: "tenant", device: "device"},
		"bad sequence":   {tenant: "tenant", device: "device", entries: []AtomicBatchEntry{{Sequence: 0, Receipt: ReceiptRow{DeviceSequence: 0, Status: "REJECTED"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.CommitBatch(context.Background(), tc.tenant, tc.device, tc.entries)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("err=%v, want ErrInvalid", err)
			}
		})
	}
}

func atomicBatchEntry(seq int64, id, digest string) AtomicBatchEntry {
	at := time.Unix(100+seq, 0).UTC()
	payload := json.RawMessage(`{"state":"OPEN"}`)
	return AtomicBatchEntry{Sequence: seq, Receipt: ReceiptRow{DeviceSequence: seq, Status: "ACCEPTED", ObservationID: id, Payload: json.RawMessage(`{"input_digest":"sha256:0123456789012345678901234567890123456789012345678901234567890123"}`)}, SessionIsNew: true,
		Session:     &SessionRow{ID: "session-" + id, TenantID: "tenant", WorkerRef: "worker-" + id, AssignmentRef: "assignment", Status: "OPEN", Source: "KIOSK", OpenedAt: at, Payload: payload},
		Event:       &EventRow{SessionID: "session-" + id, Kind: "OPENED", ActorRef: "device", IdempotencyKey: "key-" + id, Digest: digest, Payload: payload},
		Observation: &ObservationRow{ID: id, TenantID: "tenant", WorkerRef: "worker-" + id, AssignmentRef: "assignment", DeviceRef: "device", Source: "KIOSK", EventType: "IN", ProjectRef: "project", Timezone: "UTC", IdempotencyKey: "obs-" + id, Digest: digest, OccurredAt: at, ReceivedAt: at.Add(time.Minute), Payload: payload}}
}

func TestTodo_TCLOCK_003_IntegrationAtomicCommitAndReplay(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	a, b := atomicBatchEntry(1, "obs-1", "digest-1"), atomicBatchEntry(2, "obs-2", "digest-2")
	got, err := s.CommitBatch(ctx, "tenant", "device", []AtomicBatchEntry{b, a})
	if err != nil || len(got.Receipts) != 2 || got.HighestContiguous != 2 {
		t.Fatalf("commit=%+v err=%v", got, err)
	}
	if _, err := s.CurrentSession(ctx, "tenant", "worker-obs-1", "assignment"); err != nil {
		t.Fatalf("session missing: %v", err)
	}
	obs, _, err := s.ListObservations(ctx, "tenant", "worker-obs-1", time.Time{}, time.Time{}, "", 10)
	if err != nil || len(obs) != 1 || !obs[0].OccurredAt.Equal(time.Unix(101, 0).UTC()) {
		t.Fatalf("observations=%+v err=%v", obs, err)
	}
	events, err := s.ListEvents(ctx, "tenant", 0, 20)
	if err != nil || len(events) != 4 {
		t.Fatalf("outbox=%d err=%v", len(events), err)
	}
	replay, err := s.CommitBatch(ctx, "tenant", "device", []AtomicBatchEntry{a, b})
	if err != nil || len(replay.Receipts) != 2 || len(replay.Receipts[0].Payload) == 0 {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	events, _ = s.ListEvents(ctx, "tenant", 0, 20)
	if len(events) != 4 {
		t.Fatalf("replay duplicated outbox: %d", len(events))
	}
	changed := a
	changed.Observation.Digest = "changed"
	changed.Event.Digest = "changed"
	if _, err := s.CommitBatch(ctx, "tenant", "device", []AtomicBatchEntry{changed}); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed replay err=%v", err)
	}
}

func TestTodo_TCLOCK_003_FaultRollsBackEarlierRows(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	a, bad := atomicBatchEntry(1, "obs-fault-1", "digest-1"), atomicBatchEntry(2, "obs-fault-2", "digest-2")
	bad.Observation.Payload = json.RawMessage("{")
	if _, err := s.CommitBatch(ctx, "tenant", "device-fault", []AtomicBatchEntry{a, bad}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("fault err=%v", err)
	}
	if _, err := s.CurrentSession(ctx, "tenant", "worker-obs-fault-1", "assignment"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("earlier session survived rollback: %v", err)
	}
	rows, err := s.ListEvents(ctx, "tenant", 0, 20)
	if err != nil || len(rows) != 0 {
		t.Fatalf("earlier outbox survived rollback: %d %v", len(rows), err)
	}
}

func TestTodo_TCLOCK_003_BatchAdvancesExistingSelfProjectionOnce(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	entry := atomicBatchEntry(1, "obs-projection", "digest-projection")
	entry.Session.WorkerRef, entry.Observation.WorkerRef = "worker-projection", "worker-projection"
	if err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO time_self_clock_projection(tenant_id,worker_ref,assignment_ref) VALUES($1,$2,$3)`, "tenant", "worker-projection", "assignment")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitBatch(ctx, "tenant", "device-projection", []AtomicBatchEntry{entry}); err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT revision FROM time_self_clock_projection WHERE tenant_id=$1 AND worker_ref=$2 AND assignment_ref=$3`, "tenant", "worker-projection", "assignment").Scan(&revision)
	}); err != nil || revision != 2 {
		t.Fatalf("revision=%d err=%v, want 2", revision, err)
	}
	if _, err := s.CommitBatch(ctx, "tenant", "device-projection", []AtomicBatchEntry{entry}); err != nil {
		t.Fatal(err)
	}
	if err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT revision FROM time_self_clock_projection WHERE tenant_id=$1 AND worker_ref=$2 AND assignment_ref=$3`, "tenant", "worker-projection", "assignment").Scan(&revision)
	}); err != nil || revision != 2 {
		t.Fatalf("replay revision=%d err=%v, want unchanged 2", revision, err)
	}
	missing := atomicBatchEntry(2, "obs-no-projection", "digest-no-projection")
	missing.Session.WorkerRef, missing.Observation.WorkerRef = "worker-without-projection", "worker-without-projection"
	if _, err := s.CommitBatch(ctx, "tenant", "device-projection", []AtomicBatchEntry{missing}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.RunTenantTx(ctx, "tenant", func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM time_self_clock_projection WHERE tenant_id=$1 AND worker_ref=$2`, "tenant", "worker-without-projection").Scan(&count)
	}); err != nil || count != 0 {
		t.Fatalf("missing projection count=%d err=%v, want 0", count, err)
	}
}
