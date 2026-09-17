package outbox

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestTodo_EVENT_005_RejectsUnusableStore(t *testing.T) {
	ctx := context.Background()
	broken := errBrokerStore{errors.New("connection refused")}
	tenant := uuid.New()

	if err := (&PostgresBroker{db: broken, maxAttempts: 3, maxPoll: 10}).Setup(ctx); err == nil {
		t.Fatal("setup against a dead store succeeded")
	}
	broker, err := NewPostgresBroker(broken, brokerPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Publish(ctx, PublishRequest{Tenant: tenant, Stream: "s", Partition: "p", EffectIdentity: "e", Payload: []byte("x")}); err == nil {
		t.Fatal("publish against a dead store succeeded")
	}
	if _, err := broker.Poll(ctx, tenant, "c", "s", "p", 0, 1); err == nil {
		t.Fatal("poll against a dead store succeeded")
	}
	if _, _, err := broker.ReadCheckpoint(ctx, tenant, "c", "s", "p"); err == nil {
		t.Fatal("checkpoint read against a dead store succeeded")
	}
	if _, err := broker.CommitCheckpoint(ctx, ConsumerCheckpoint{Tenant: tenant, Consumer: "c", Stream: "s", Partition: "p"}); err == nil {
		t.Fatal("checkpoint commit against a dead store succeeded")
	}
	if _, err := broker.Fail(ctx, tenant, "s", "e", "boom"); err == nil {
		t.Fatal("fail against a dead store succeeded")
	}
	if _, err := broker.DeadLetters(ctx, tenant, "s", "p", 1); err == nil {
		t.Fatal("dead-letter read against a dead store succeeded")
	}
	if !isUniqueViolation(errors.New(`ERROR: duplicate key value violates unique constraint "x" (SQLSTATE 23505)`)) {
		t.Fatal("unique violation not recognized")
	}
	if isUniqueViolation(nil) || isUniqueViolation(errors.New("connection refused")) {
		t.Fatal("benign error mistaken for a unique violation")
	}
}

func postgresBroker(t *testing.T) *PostgresBroker {
	t.Helper()
	db := pgtest.New(t)
	broker, err := NewPostgresBroker(db.Conn, brokerPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.Setup(context.Background()); err != nil {
		t.Fatal(err)
	}
	return broker
}

// TestTodo_EVENT_005_Integration proves the Postgres backend against a
// real database: the round trip is durable across broker handles, and
// concurrent publishers converge exactly like the memory double.
func TestTodo_EVENT_005_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	broker, err := NewPostgresBroker(db.Conn, brokerPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if err := broker.Setup(ctx); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()

	first, err := broker.Publish(ctx, PublishRequest{Tenant: tenant, Stream: "payroll", Partition: "eu", EffectIdentity: "pg-1", OrderingKey: "eu/1", Payload: []byte("one"), SchemaRef: "hcm/pay/v1"})
	if err != nil || first.Offset != 1 {
		t.Fatalf("publish = %+v err=%v", first, err)
	}
	second, err := broker.Publish(ctx, PublishRequest{Tenant: tenant, Stream: "payroll", Partition: "eu", EffectIdentity: "pg-2", Payload: []byte("two")})
	if err != nil || second.Offset != 2 {
		t.Fatalf("second publish = %+v err=%v", second, err)
	}
	if _, err := broker.CommitCheckpoint(ctx, ConsumerCheckpoint{Tenant: tenant, Consumer: "worker", Stream: "payroll", Partition: "eu", Offset: 2}); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := broker.Fail(ctx, tenant, "payroll", "pg-2", "downstream 500"); err != nil {
			t.Fatal(err)
		}
	}

	// A fresh handle over the same database sees every durable row: the
	// Postgres backend is storage, not process memory.
	reopened, err := NewPostgresBroker(db.Conn, brokerPolicy())
	if err != nil {
		t.Fatal(err)
	}
	page, err := reopened.Poll(ctx, tenant, "worker", "payroll", "eu", 0, 10)
	if err != nil || len(page) != 2 || page[0].EffectIdentity != "pg-1" || page[1].EffectIdentity != "pg-2" {
		t.Fatalf("reopened poll = %+v err=%v", page, err)
	}
	position, found, err := reopened.ReadCheckpoint(ctx, tenant, "worker", "payroll", "eu")
	if err != nil || !found || position.Offset != 2 {
		t.Fatalf("reopened checkpoint = %+v found=%v err=%v", position, found, err)
	}
	letters, err := reopened.DeadLetters(ctx, tenant, "payroll", "eu", 10)
	if err != nil || len(letters) != 1 || letters[0].Attempts != 3 {
		t.Fatalf("reopened dead letters = %+v err=%v", letters, err)
	}
	redelivery, err := reopened.Publish(ctx, PublishRequest{Tenant: tenant, Stream: "payroll", Partition: "eu", EffectIdentity: "pg-1", Payload: []byte("changed")})
	if err != nil || !redelivery.Duplicate || redelivery.Offset != 1 {
		t.Fatalf("reopened redelivery diverged: %+v err=%v", redelivery, err)
	}

	// Concurrent publishers on one partition converge: one offset per
	// identity, no gaps, identical seals.
	const publishers = 8
	var wg sync.WaitGroup
	offsets := make([]int64, publishers)
	errs := make([]error, publishers)
	for i := 0; i < publishers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			record, err := reopened.Publish(ctx, PublishRequest{Tenant: tenant, Stream: "race", Partition: "p", EffectIdentity: "pg-race", Payload: []byte("x")})
			_ = record
			_ = err
			private, perr := reopened.Publish(ctx, PublishRequest{Tenant: tenant, Stream: "race", Partition: "q", EffectIdentity: string(rune('a' + i)), Payload: []byte("x")})
			if perr == nil {
				offsets[i] = private.Offset
			}
			errs[i] = perr
		}(i)
	}
	wg.Wait()
	seen := map[int64]bool{}
	for i := 0; i < publishers; i++ {
		if errs[i] != nil {
			t.Fatalf("publisher %d: %v", i, errs[i])
		}
		if offsets[i] < 1 || seen[offsets[i]] {
			t.Fatalf("publisher %d offset %d is missing or duplicated", i, offsets[i])
		}
		seen[offsets[i]] = true
	}
}
