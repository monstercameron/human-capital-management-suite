package idempotencystore

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func insertIdemTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	tenant := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`,
		tenant, key, "tenant "+key)
	return tenant
}

func idemAppConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func idemFixture(t *testing.T) (*Store, *pgtest.DB, uuid.UUID) {
	t.Helper()
	db := pgtest.New(t)
	tenant := insertIdemTenant(t, db, "idem-primary")
	return New(idemAppConn(t, db)), db, tenant
}

// TestTodo_INTAPI_005 proves the durable exactly-once core INTAPI-005
// needs: the first claim of a key wins and executes, a repeat with the
// same digest replays the original result, a repeat with a different
// digest is refused, an expired key is reclaimable, keys never cross
// tenants, and completing an unknown or finished claim fails.
func TestTodo_INTAPI_005(t *testing.T) {
	ctx := context.Background()
	store, db, tenant := idemFixture(t)
	other := insertIdemTenant(t, db, "idem-second")

	outcome, replayed, err := store.Claim(ctx, tenant, "hire", "key-1", "digest-a", time.Hour)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if outcome != OutcomeExecute {
		t.Fatalf("first claim outcome = %v, want Execute", outcome)
	}

	outcome, _, err = store.Claim(ctx, tenant, "hire", "key-1", "digest-a", time.Hour)
	if err != nil {
		t.Fatalf("in-flight claim: %v", err)
	}
	if outcome != OutcomeInFlight {
		t.Fatalf("in-flight claim outcome = %v, want InFlight", outcome)
	}

	want := []byte(`{"status":"hired"}`)
	if err := store.Complete(ctx, tenant, "hire", "key-1", want); err != nil {
		t.Fatalf("complete: %v", err)
	}

	outcome, replayed, err = store.Claim(ctx, tenant, "hire", "key-1", "digest-a", time.Hour)
	if err != nil {
		t.Fatalf("replay claim: %v", err)
	}
	if outcome != OutcomeReplay {
		t.Fatalf("replay claim outcome = %v, want Replay", outcome)
	}
	if string(replayed.Result) != string(want) {
		t.Fatalf("replay result = %q, want the original", replayed.Result)
	}

	if _, _, err := store.Claim(ctx, tenant, "hire", "key-1", "digest-b", time.Hour); !errors.Is(err, ErrDigestConflict) {
		t.Fatalf("conflicting digest = %v, want digest conflict", err)
	}

	outcome, _, err = store.Claim(ctx, other, "hire", "key-1", "digest-a", time.Hour)
	if err != nil {
		t.Fatalf("other-tenant claim: %v", err)
	}
	if outcome != OutcomeExecute {
		t.Fatalf("other-tenant claim outcome = %v, want Execute", outcome)
	}

	db.Exec(t, `
		INSERT INTO idempotency_key (tenant_id, capability, key, request_digest, status, expires_at)
		VALUES ($1,'hire','key-old','digest-old','in_progress',timestamptz '2020-01-01T00:00:00Z')`, tenant)
	outcome, _, err = store.Claim(ctx, tenant, "hire", "key-old", "digest-new", time.Hour)
	if err != nil {
		t.Fatalf("expired reclaim: %v", err)
	}
	if outcome != OutcomeExecute {
		t.Fatalf("expired reclaim outcome = %v, want Execute", outcome)
	}

	if err := store.Complete(ctx, tenant, "hire", "key-ghost", want); !errors.Is(err, ErrNoClaim) {
		t.Fatalf("complete unknown = %v, want no-claim", err)
	}
	if err := store.Complete(ctx, tenant, "hire", "key-1", want); !errors.Is(err, ErrNoClaim) {
		t.Fatalf("double complete = %v, want no-claim", err)
	}
	if _, _, err := store.Claim(ctx, tenant, "", "key-2", "digest-a", time.Hour); CodeOf(err) != CodeInvalid {
		t.Fatalf("blank capability = %v, want INVALID", err)
	}
	if _, _, err := store.Claim(ctx, uuid.Nil, "hire", "key-2", "digest-a", time.Hour); CodeOf(err) != CodeTenantRequired {
		t.Fatalf("nil tenant = %v, want TENANT_REQUIRED", err)
	}
}

// TestTodo_INTAPI_005_Race proves concurrent duplicates execute once:
// sixteen racers claiming one key produce exactly one winner, and after
// the winner completes every racer replays the same original result.
func TestTodo_INTAPI_005_Race(t *testing.T) {
	ctx := context.Background()
	_, db, tenant := idemFixture(t)

	const racers = 16
	var executed atomic.Int32
	var wg sync.WaitGroup
	errs := make([]error, racers)
	outcomes := make([]Outcome, racers)
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			s := New(idemAppConn(t, db))
			outcome, _, err := s.Claim(ctx, tenant, "hire", "key-race", "digest-r", time.Hour)
			errs[i] = err
			outcomes[i] = outcome
			if err == nil && outcome == OutcomeExecute {
				executed.Add(1)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	for i := 0; i < racers; i++ {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
	}
	if got := executed.Load(); got != 1 {
		t.Fatalf("winners = %d, want exactly one", got)
	}

	winner := New(idemAppConn(t, db))
	want := []byte(`{"status":"hired"}`)
	if err := winner.Complete(ctx, tenant, "hire", "key-race", want); err != nil {
		t.Fatalf("winner complete: %v", err)
	}
	for i := 0; i < racers; i++ {
		s := New(idemAppConn(t, db))
		outcome, replayed, err := s.Claim(ctx, tenant, "hire", "key-race", "digest-r", time.Hour)
		if err != nil {
			t.Fatalf("post-race replay %d: %v", i, err)
		}
		if outcome != OutcomeReplay || string(replayed.Result) != string(want) {
			t.Fatalf("post-race replay %d = (%v, %q), want (Replay, original)", i, outcome, replayed.Result)
		}
	}
}

// TestTodo_INTAPI_005_Fault proves the store fails closed on misuse: a
// conflicting digest never overwrites the in-flight claim, completing a
// foreign-tenant key is impossible, and invalid inputs are refused before
// any write.
func TestTodo_INTAPI_005_Fault(t *testing.T) {
	ctx := context.Background()
	store, db, tenant := idemFixture(t)
	other := insertIdemTenant(t, db, "idem-fault-second")

	outcome, _, err := store.Claim(ctx, tenant, "hire", "key-f", "digest-1", time.Hour)
	if err != nil || outcome != OutcomeExecute {
		t.Fatalf("claim = (%v, %v), want (Execute, nil)", outcome, err)
	}
	if _, _, err := store.Claim(ctx, tenant, "hire", "key-f", "digest-2", time.Hour); !errors.Is(err, ErrDigestConflict) {
		t.Fatalf("conflict = %v, want digest conflict", err)
	}
	if err := store.Complete(ctx, other, "hire", "key-f", []byte(`{}`)); !errors.Is(err, ErrNoClaim) {
		t.Fatalf("foreign-tenant complete = %v, want no-claim", err)
	}
	if _, _, err := store.Claim(ctx, tenant, "hire", "", "digest-1", time.Hour); CodeOf(err) != CodeInvalid {
		t.Fatalf("blank key = %v, want INVALID", err)
	}
	if _, _, err := store.Claim(ctx, tenant, "hire", "key-g", "", time.Hour); CodeOf(err) != CodeInvalid {
		t.Fatalf("blank digest = %v, want INVALID", err)
	}
	if _, _, err := store.Claim(ctx, tenant, "hire", "key-g", "digest-1", 0); CodeOf(err) != CodeInvalid {
		t.Fatalf("zero ttl = %v, want INVALID", err)
	}
	if err := store.Complete(ctx, tenant, "hire", "key-f", nil); err != nil {
		t.Fatalf("complete with empty result: %v", err)
	}
}
