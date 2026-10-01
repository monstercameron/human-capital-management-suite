package timestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/pressly/goose/v3"
)

// migratedDB returns a migrated schema without opening a Store on it, so
// TestTodo_FTIME_002_Recovery can open and close independent Stores against
// the same schema to simulate a process restart.
func migratedDB(t *testing.T) *pgtest.DB {
	t.Helper()
	db := pgtest.NewEmpty(t)
	migrationFS, err := fs.Sub(Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db.SQL, migrationFS, goose.WithVerbose(false), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	return db
}

func newOpenSession(id, tenant, worker, assignment string, opened time.Time) (SessionRow, EventRow) {
	session := SessionRow{
		ID: id, TenantID: tenant, WorkerRef: worker, AssignmentRef: assignment,
		Status: "OPEN", Source: "PUNCH_DEVICE", OpenedAt: opened,
		Payload: json.RawMessage(`{"device":"kiosk-1"}`),
	}
	event := EventRow{
		SessionID: id, Kind: "OPENED", ActorRef: worker,
		IdempotencyKey: "open:" + id, Digest: "sha256:open-" + id,
		Payload: json.RawMessage(`{"reason":"clock_in"}`),
	}
	return session, event
}

func TestTodo_FTIME_002(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-a"
	worker, assignment := "worker-1", "assignment-1"
	opened := time.Now().UTC().Truncate(time.Second)

	session, event := newOpenSession(uuid.NewString(), tenant, worker, assignment, opened)
	created, err := s.OpenSession(ctx, tenant, session, event)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if created.Revision != 1 || created.Status != "OPEN" || !created.OpenedAt.Equal(opened) {
		t.Fatalf("created session = %+v", created)
	}

	current, err := s.CurrentSession(ctx, tenant, worker, assignment)
	if err != nil || current.ID != created.ID {
		t.Fatalf("CurrentSession = %+v, %v", current, err)
	}

	// A second, different session for the same worker/assignment must fail:
	// the RED clause "two simultaneous clock-ins both succeed" must not
	// happen even sequentially.
	other, otherEvent := newOpenSession(uuid.NewString(), tenant, worker, assignment, opened)
	if _, err := s.OpenSession(ctx, tenant, other, otherEvent); !errors.Is(err, ErrSessionAlreadyOpen) {
		t.Fatalf("second open on same worker/assignment: %v", err)
	}

	// Idempotent replay: same session id and same event digest returns the
	// original row without creating a duplicate or erroring.
	replay, err := s.OpenSession(ctx, tenant, session, event)
	if err != nil || replay.ID != created.ID || replay.Revision != 1 {
		t.Fatalf("idempotent OpenSession replay = %+v, %v", replay, err)
	}

	// Same session id, different digest: the RED clause "a retry creates
	// duplicate labor time" by a different route -- must conflict, not merge.
	conflicting := event
	conflicting.Digest = "sha256:different"
	if _, err := s.OpenSession(ctx, tenant, session, conflicting); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("mismatched digest replay: %v", err)
	}

	// Close the session through a revision-checked transition.
	closeEvent := EventRow{
		SessionID: created.ID, Kind: "CLOSED", ActorRef: worker,
		IdempotencyKey: "close:" + created.ID, Digest: "sha256:close-" + created.ID,
		Payload: json.RawMessage(`{"reason":"clock_out"}`),
	}
	next := SessionRow{ID: created.ID, TenantID: tenant, Status: "CLOSED", Source: session.Source, ClosedAt: opened.Add(8 * time.Hour), Payload: session.Payload}
	closed, err := s.ApplySessionTransition(ctx, tenant, created.ID, 1, next, []EventRow{closeEvent})
	if err != nil {
		t.Fatalf("ApplySessionTransition: %v", err)
	}
	if closed.Status != "CLOSED" || closed.Revision != 2 || closed.ClosedAt.IsZero() {
		t.Fatalf("closed session = %+v", closed)
	}

	// Stale expected revision is rejected: a distinct idempotency key and
	// digest so this exercises the revision check, not the replay path.
	staleEvent := EventRow{
		SessionID: created.ID, Kind: "CLOSED", ActorRef: worker,
		IdempotencyKey: "close-stale:" + created.ID, Digest: "sha256:close-stale-" + created.ID,
		Payload: json.RawMessage(`{"reason":"clock_out_retry"}`),
	}
	if _, err := s.ApplySessionTransition(ctx, tenant, created.ID, 1, next, []EventRow{staleEvent}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale revision: %v", err)
	}

	// A genuine retry (same idempotency key and digest as the original
	// close) replays the prior result instead of erroring or reapplying.
	replayClosed, err := s.ApplySessionTransition(ctx, tenant, created.ID, 1, next, []EventRow{closeEvent})
	if err != nil || replayClosed.Revision != 2 {
		t.Fatalf("idempotent close replay = %+v, %v", replayClosed, err)
	}

	// A closed session is no longer the current one, and a fresh open on
	// the same worker/assignment now succeeds.
	if _, err := s.CurrentSession(ctx, tenant, worker, assignment); !errors.Is(err, ErrNotFound) {
		t.Fatalf("current session after close: %v", err)
	}
	reopened, reopenEvent := newOpenSession(uuid.NewString(), tenant, worker, assignment, opened.Add(9*time.Hour))
	if _, err := s.OpenSession(ctx, tenant, reopened, reopenEvent); err != nil {
		t.Fatalf("reopen after close: %v", err)
	}

	list, cursor, err := s.ListSessions(ctx, tenant, worker, time.Time{}, time.Time{}, "", 10)
	if err != nil || len(list) != 2 || cursor != "" {
		t.Fatalf("ListSessions = %d items, cursor=%q, err=%v", len(list), cursor, err)
	}
}

func TestCurrentSession_OnBreakRemainsActiveUntilClockOut(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant, worker, assignment := "tenant-break", "worker-break", "assignment-break"
	opened := time.Now().UTC().Truncate(time.Second)
	session, event := newOpenSession(uuid.NewString(), tenant, worker, assignment, opened)
	created, err := s.OpenSession(ctx, tenant, session, event)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	breakEvent := EventRow{
		SessionID: created.ID, Kind: "BREAK_START", ActorRef: worker,
		IdempotencyKey: "break:" + created.ID, Digest: "sha256:break-" + created.ID,
		Payload: json.RawMessage(`{"reason":"rest"}`),
	}
	onBreak := SessionRow{ID: created.ID, TenantID: tenant, Status: "ON_BREAK", Source: session.Source, Payload: session.Payload}
	updated, err := s.ApplySessionTransition(ctx, tenant, created.ID, 1, onBreak, []EventRow{breakEvent})
	if err != nil || updated.Status != "ON_BREAK" {
		t.Fatalf("ApplySessionTransition to break: %+v, %v", updated, err)
	}
	current, err := s.CurrentSession(ctx, tenant, worker, assignment)
	if err != nil || current.ID != created.ID || current.Status != "ON_BREAK" {
		t.Fatalf("CurrentSession during break: %+v, %v", current, err)
	}
	second, secondEvent := newOpenSession(uuid.NewString(), tenant, worker, assignment, opened.Add(time.Minute))
	if _, err := s.OpenSession(ctx, tenant, second, secondEvent); !errors.Is(err, ErrSessionAlreadyOpen) {
		t.Fatalf("clock-in during break: %v", err)
	}
	outEvent := EventRow{
		SessionID: created.ID, Kind: "CLOSED", ActorRef: worker,
		IdempotencyKey: "out:" + created.ID, Digest: "sha256:out-" + created.ID,
		Payload: json.RawMessage(`{"reason":"clock_out"}`),
	}
	closed := SessionRow{ID: created.ID, TenantID: tenant, Status: "CLOSED", Source: session.Source, ClosedAt: opened.Add(time.Hour), Payload: session.Payload}
	if _, err := s.ApplySessionTransition(ctx, tenant, created.ID, updated.Revision, closed, []EventRow{outEvent}); err != nil {
		t.Fatalf("clock out from break: %v", err)
	}
}

func TestTodo_FTIME_002_Integration(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-int"
	worker, assignment := "worker-int", "assignment-int"
	opened := time.Now().UTC().Truncate(time.Second)

	session, event := newOpenSession(uuid.NewString(), tenant, worker, assignment, opened)
	created, err := s.OpenSession(ctx, tenant, session, event)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	obs := ObservationRow{
		ID: uuid.NewString(), TenantID: tenant, WorkerRef: worker, AssignmentRef: assignment,
		DeviceRef: "kiosk-1", Source: "KIOSK", EventType: "CLOCK_IN", ProjectRef: "project-1", Timezone: "UTC",
		OccurredAt: opened, ReceivedAt: opened.Add(time.Second),
		IdempotencyKey: "punch:1", Digest: "sha256:punch-1", Payload: json.RawMessage(`{"seq":1}`),
	}
	stored, duplicate, err := s.AppendObservation(ctx, tenant, obs)
	if err != nil || duplicate {
		t.Fatalf("AppendObservation: %+v dup=%v err=%v", stored, duplicate, err)
	}

	// A correction against the original observation, carrying lineage.
	correction := obs
	correction.ID = uuid.NewString()
	correction.CorrectsID = stored.ID
	correction.IdempotencyKey = "punch:1-correction"
	correction.Digest = "sha256:punch-1-correction"
	correction.OccurredAt = opened.Add(-time.Minute)
	correctionRow, dup, err := s.AppendObservation(ctx, tenant, correction)
	if err != nil || dup || correctionRow.CorrectsID != stored.ID {
		t.Fatalf("correction observation = %+v dup=%v err=%v", correctionRow, dup, err)
	}

	// A correction naming an observation that does not exist is rejected,
	// not silently accepted as first-party evidence.
	bogus := correction
	bogus.ID = uuid.NewString()
	bogus.CorrectsID = uuid.NewString()
	bogus.IdempotencyKey = "punch:bogus"
	bogus.Digest = "sha256:bogus"
	if _, _, err := s.AppendObservation(ctx, tenant, bogus); !errors.Is(err, ErrNotFound) {
		t.Fatalf("correction of missing observation: %v", err)
	}

	events, err := s.ListEvents(ctx, tenant, 0, 100)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) < 3 {
		t.Fatalf("expected session-opened plus two punch outbox events, got %d", len(events))
	}
	if events[0].EventType != "clock.session.opened" {
		t.Fatalf("first outbox event = %s", events[0].EventType)
	}

	list, _, err := s.ListObservations(ctx, tenant, worker, time.Time{}, time.Time{}, "", 10)
	if err != nil || len(list) != 2 {
		t.Fatalf("ListObservations = %d, %v", len(list), err)
	}
	_ = created
}

func TestTodo_FTIME_002_Race(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	tenant := "tenant-race"
	worker, assignment := "worker-race", "assignment-race"
	opened := time.Now().UTC().Truncate(time.Second)

	const attempts = 8
	var wg sync.WaitGroup
	results := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			session, event := newOpenSession(uuid.NewString(), tenant, worker, assignment, opened)
			_, err := s.OpenSession(ctx, tenant, session, event)
			results[i] = err
		}(i)
	}
	wg.Wait()

	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, ErrSessionAlreadyOpen) {
			t.Fatalf("unexpected race error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("exactly one concurrent clock-in should win, got %d", successes)
	}
}

func TestTodo_FTIME_002_Recovery(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	cfg := Config{DSN: db.URL, Schema: db.Schema}

	s1, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	tenant := "tenant-recover"
	worker, assignment := "worker-recover", "assignment-recover"
	opened := time.Now().UTC().Truncate(time.Second)
	session, event := newOpenSession(uuid.NewString(), tenant, worker, assignment, opened)
	created, err := s1.OpenSession(ctx, tenant, session, event)
	if err != nil {
		t.Fatalf("OpenSession before restart: %v", err)
	}
	before, err := s1.ListEvents(ctx, tenant, 0, 10)
	if err != nil || len(before) != 1 {
		t.Fatalf("outbox before restart = %d, %v", len(before), err)
	}
	s1.Close()

	// Simulate a process restart: a fresh Store reconnects to the same
	// schema. The open session and its unsent outbox row must survive.
	s2, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("New after restart: %v", err)
	}
	defer s2.Close()

	current, err := s2.CurrentSession(ctx, tenant, worker, assignment)
	if err != nil || current.ID != created.ID {
		t.Fatalf("CurrentSession after restart = %+v, %v", current, err)
	}

	// The outbox row survives and can be re-read again as if a publisher
	// had crashed before consuming it: ListEvents does not consume rows.
	after, err := s2.ListEvents(ctx, tenant, 0, 10)
	if err != nil || len(after) != 1 || after[0].Sequence != before[0].Sequence {
		t.Fatalf("outbox after restart = %+v, %v", after, err)
	}
	again, err := s2.ListEvents(ctx, tenant, 0, 10)
	if err != nil || len(again) != 1 {
		t.Fatalf("outbox re-read after failed publisher = %+v, %v", again, err)
	}
}

// createTimeRLSRole creates a NOBYPASSRLS role granted the same table
// privileges the application uses, so a test can prove enforcement instead
// of the store's own connection, which is a superuser and would silently
// bypass RLS regardless of FORCE ROW LEVEL SECURITY.
func createTimeRLSRole(t *testing.T, s *Store, schema string) string {
	t.Helper()
	ctx := context.Background()
	role := "time_rls_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedRole, quotedSchema := quoteTimeIdentifier(role), quoteTimeIdentifier(schema)
	err := s.RunTenantTx(ctx, "setup", func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `CREATE ROLE `+quotedRole+` NOLOGIN NOSUPERUSER NOBYPASSRLS`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `GRANT USAGE ON SCHEMA `+quotedSchema+` TO `+quotedRole); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA `+quotedSchema+` TO `+quotedRole)
		return err
	})
	if err != nil {
		t.Fatalf("create restricted time role: %v", err)
	}
	t.Cleanup(func() {
		_ = s.RunTenantTx(context.Background(), "setup", func(tx dbport.Tx) error {
			if _, err := tx.Exec(context.Background(), `DROP OWNED BY `+quotedRole); err != nil {
				return err
			}
			_, err := tx.Exec(context.Background(), `DROP ROLE `+quotedRole)
			return err
		})
	})
	return role
}

func quoteTimeIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func TestTodo_FTIME_002_Security(t *testing.T) {
	db := migratedDB(t)
	ctx := context.Background()
	s, err := New(ctx, Config{DSN: db.URL, Schema: db.Schema})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()
	role := createTimeRLSRole(t, s, db.Schema)

	opened := time.Now().UTC().Truncate(time.Second)
	session, event := newOpenSession(uuid.NewString(), "tenant-a", "worker-1", "assignment-1", opened)
	created, err := s.OpenSession(ctx, "tenant-a", session, event)
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}

	// Tenant B cannot see tenant A's open session by worker/assignment,
	// through the ordinary application path.
	if _, err := s.CurrentSession(ctx, "tenant-b", "worker-1", "assignment-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant CurrentSession: %v", err)
	}

	// RLS is proven directly under a role with no BYPASSRLS privilege:
	// querying tenant A's row while scoped to tenant B, even with the
	// exact guessed id, returns no rows.
	err = s.RunTenantTx(ctx, "tenant-b", func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL ROLE `+quoteTimeIdentifier(role)); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM time_session WHERE id=$1`, created.ID).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("tenant B saw %d rows for tenant A's session id", count)
		}
		var total int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM time_session`).Scan(&total); err != nil {
			return err
		}
		if total != 0 {
			return fmt.Errorf("tenant B saw %d rows total; tenant A's row leaked", total)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("tenant-b restricted-role tx: %v", err)
	}

	// The same restricted role cannot forge a tenant-a row while scoped to
	// tenant-b: the WITH CHECK clause rejects the write.
	err = s.RunTenantTx(ctx, "tenant-b", func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL ROLE `+quoteTimeIdentifier(role)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO time_session(tenant_id,id,worker_ref,assignment_ref,status,source,project_ref,revision,opened_at,payload) VALUES('tenant-a','forged','attacker','assignment-1','OPEN','PUNCH_DEVICE','',1,now(),'{}'::jsonb)`)
		return err
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "row-level security") {
		t.Fatalf("forged cross-tenant insert error = %v; want row-level security rejection", err)
	}
}
