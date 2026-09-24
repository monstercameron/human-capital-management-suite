package schema_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestTodo_LEDGER_001 proves the ledger tables: stream identity is semantic and
// tenant scoped, sequences are unique and start at one, an event cannot omit its
// assertion class, source, schema, times or digest, and events are append-only.
func TestTodo_LEDGER_001(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)
	fixture := newLedgerFixture(t, db)

	t.Run("ledger_event is partitioned by tenant", func(t *testing.T) {
		var kind string
		if err := db.QueryRow(ctx, `
			SELECT c.relkind::text FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = current_schema() AND c.relname = 'ledger_event'`).Scan(&kind); err != nil {
			t.Fatalf("read ledger_event: %v", err)
		}
		if kind != "p" {
			t.Fatalf("ledger_event relkind is %q, want a partitioned table", kind)
		}

		var strategy string
		var partitions int
		if err := db.QueryRow(ctx, `
			SELECT p.partstrat::text,
			       (SELECT count(*) FROM pg_inherits i WHERE i.inhparent = p.partrelid)
			FROM pg_partitioned_table p
			JOIN pg_class c ON c.oid = p.partrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = current_schema() AND c.relname = 'ledger_event'`).
			Scan(&strategy, &partitions); err != nil {
			t.Fatalf("read partition strategy: %v", err)
		}
		if strategy != "h" {
			t.Fatalf("ledger_event partition strategy is %q, want hash", strategy)
		}
		if partitions != 4 {
			t.Fatalf("ledger_event has %d partitions, want 4", partitions)
		}
	})

	t.Run("sequences are unique and start at one", func(t *testing.T) {
		fixture.append(t, fixture.event(1, "TRANSACTION_FACT"))

		duplicate := fixture.event(1, "TRANSACTION_FACT")
		if err := fixture.appendErr(duplicate); err == nil {
			t.Fatal("a duplicate tenant/stream/sequence was accepted")
		}
		if err := fixture.appendErr(fixture.event(0, "TRANSACTION_FACT")); err == nil {
			t.Fatal("sequence 0 was accepted")
		}
		if err := fixture.appendErr(fixture.event(-1, "TRANSACTION_FACT")); err == nil {
			t.Fatal("a negative sequence was accepted")
		}
	})

	t.Run("an event cannot omit its envelope", func(t *testing.T) {
		required := []string{
			"assertion_class", "source_ref", "schema_ref", "digest",
			"digest_algorithm", "occurred_at", "effective_at", "recorded_at", "correlation_id",
		}
		for _, column := range required {
			var nullable string
			if err := db.QueryRow(ctx, `
				SELECT is_nullable FROM information_schema.columns
				WHERE table_schema = $1 AND table_name = 'ledger_event' AND column_name = $2`,
				db.Schema, column).Scan(&nullable); err != nil {
				t.Fatalf("ledger_event has no %s column: %v", column, err)
			}
			if nullable != "NO" {
				t.Fatalf("ledger_event.%s is nullable", column)
			}
		}

		if err := db.ExecErr(`
			INSERT INTO ledger_event (
				tenant_id, stream_key, sequence, event_id, assertion_class, source_ref,
				schema_ref, payload, canonical_length, digest, digest_algorithm,
				occurred_at, effective_at, correlation_id, idempotency_key)
			VALUES ($1, $2, 9, $3, 'TRANSACTION_FACT', 'test', $4, '\x00', 1, $5, 'sha256',
				now(), now(), $6, $7)`,
			fixture.tenant, fixture.streamKey, uuid.New(), "unregistered.schema@1",
			fixtureDigestA, uuid.New(), uuid.NewString()); err == nil {
			t.Fatal("an event referencing an unregistered payload schema was accepted")
		}

		if err := db.ExecErr(`
			INSERT INTO ledger_event (
				tenant_id, stream_key, sequence, event_id, assertion_class, source_ref,
				schema_ref, payload, canonical_length, digest, digest_algorithm,
				occurred_at, effective_at, correlation_id, idempotency_key)
			VALUES ($1, 'unregistered-stream', 1, $2, 'TRANSACTION_FACT', 'test', $3, '\x00', 1, $4,
				'sha256', now(), now(), $5, $6)`,
			fixture.tenant, uuid.New(), fixture.schemaRef, fixtureDigestA,
			uuid.New(), uuid.NewString()); err == nil {
			t.Fatal("an event on an unregistered stream was accepted")
		}
	})

	t.Run("assertion classes are exactly the five declared", func(t *testing.T) {
		classes := []string{
			"TRANSACTION_FACT", "DOMAIN_FACT", "EXTERNAL_OBSERVATION", "CLAIM", "CORRECTION",
		}
		for i, class := range classes {
			if err := fixture.appendErr(fixture.event(int64(100+i), class)); err != nil {
				t.Fatalf("assertion class %s was rejected: %v", class, err)
			}
		}
		if err := fixture.appendErr(fixture.event(200, "OPINION")); err == nil {
			t.Fatal("an undeclared assertion class was accepted")
		}
	})

	t.Run("events are append-only", func(t *testing.T) {
		if err := db.ExecErr(`UPDATE ledger_event SET source_ref = 'forged'`); err == nil {
			t.Fatal("a ledger event was updated in place")
		}
		if err := db.ExecErr(`DELETE FROM ledger_event`); err == nil {
			t.Fatal("a ledger event was deleted")
		}
		// The append-only trigger is cloned onto each partition, so addressing the
		// partition that actually holds the row is no way around it. A row trigger
		// can only refuse rows that exist, so the partition must be the right one.
		var partition string
		if err := db.QueryRow(ctx, `
			SELECT tableoid::regclass::text FROM ledger_event
			WHERE tenant_id = $1 AND stream_key = $2 LIMIT 1`,
			fixture.tenant, fixture.streamKey).Scan(&partition); err != nil {
			t.Fatalf("locate the partition holding the event: %v", err)
		}
		if err := db.ExecErr(`DELETE FROM ` + partition); err == nil {
			t.Fatalf("a ledger event was deleted through partition %s", partition)
		}
		if err := db.ExecErr(`UPDATE ` + partition + ` SET source_ref = 'forged'`); err == nil {
			t.Fatalf("a ledger event was updated through partition %s", partition)
		}
	})

	t.Run("the stream head stays coherent", func(t *testing.T) {
		if err := db.ExecErr(`
			UPDATE stream_head SET head_sequence = 3 WHERE tenant_id = $1 AND stream_key = $2`,
			fixture.tenant, fixture.streamKey); err == nil {
			t.Fatal("a head advanced past zero without a head digest")
		}
		db.Exec(t, `
			UPDATE stream_head SET head_sequence = 3, head_digest = $3, head_digest_algorithm = 'sha256'
			WHERE tenant_id = $1 AND stream_key = $2`,
			fixture.tenant, fixture.streamKey, fixtureDigestA)
		if err := db.ExecErr(`
			UPDATE stream_head SET head_sequence = -1 WHERE tenant_id = $1 AND stream_key = $2`,
			fixture.tenant, fixture.streamKey); err == nil {
			t.Fatal("a negative head sequence was accepted")
		}
	})
}

// TestTodo_LEDGER_001_Mutation proves that the append-only rule protects the
// exact persisted bytes, including the digest and event identity.
func TestTodo_LEDGER_001_Mutation(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	f := newLedgerFixture(t, db)
	f.append(t, f.event(1, "TRANSACTION_FACT"))

	type row struct {
		eventID uuid.UUID
		digest  string
		payload []byte
	}
	read := func() row {
		t.Helper()
		var got row
		if err := db.QueryRow(context.Background(), `
			SELECT event_id, digest, payload FROM ledger_event
			WHERE tenant_id = $1 AND stream_key = $2 AND sequence = 1`, f.tenant, f.streamKey).
			Scan(&got.eventID, &got.digest, &got.payload); err != nil {
			t.Fatalf("read ledger event: %v", err)
		}
		return got
	}
	before := read()
	if err := db.ExecErr(`UPDATE ledger_event SET payload = $1, digest = $2
		WHERE tenant_id = $3 AND stream_key = $4 AND sequence = 1`, []byte("forged"), fixtureDigestB, f.tenant, f.streamKey); err == nil {
		t.Fatal("mutating persisted payload and digest succeeded")
	}
	if err := db.ExecErr(`DELETE FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2 AND sequence = 1`, f.tenant, f.streamKey); err == nil {
		t.Fatal("deleting the persisted event succeeded")
	}
	after := read()
	if before.eventID != after.eventID || before.digest != after.digest || string(before.payload) != string(after.payload) {
		t.Fatalf("event changed after refused mutation: before=%+v after=%+v", before, after)
	}
}

// TestTodo_LEDGER_001_Race races two writes for the same tenant/stream/sequence.
// The unique key must serialize them into exactly one accepted event.
func TestTodo_LEDGER_001_Race(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	f := newLedgerFixture(t, db)
	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		conn := db.NewConn(t)
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := conn.Exec(context.Background(), `
				INSERT INTO ledger_event (
					tenant_id, stream_key, sequence, event_id, assertion_class, source_ref,
					schema_ref, payload, canonical_length, digest, digest_algorithm,
					occurred_at, effective_at, correlation_id, idempotency_key)
				VALUES ($1, $2, 1, $3, 'TRANSACTION_FACT', 'test', $4, $5, 4, $6,
					'sha256', now(), now(), $7, $8)`, f.tenant, f.streamKey, uuid.New(),
				f.schemaRef, []byte("body"), fixtureDigestA, uuid.New(), uuid.NewString())
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	accepted, rejected := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else {
			rejected++
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("concurrent writes accepted=%d rejected=%d, want one of each", accepted, rejected)
	}
	var count int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1 AND stream_key = $2`, f.tenant, f.streamKey).Scan(&count); err != nil {
		t.Fatalf("count persisted events: %v", err)
	}
	if count != 1 {
		t.Fatalf("persisted %d events after same-sequence race, want 1", count)
	}
}

// TestTodo_LEDGER_001_Security proves the authority and payload rules the
// schema itself must carry: an authority-bearing class needs an assignment, a
// correction needs a target, and large bytes never enter the envelope.
func TestTodo_LEDGER_001_Security(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	fixture := newLedgerFixture(t, db)

	t.Run("a domain fact needs an authority", func(t *testing.T) {
		spec := fixture.event(1, "DOMAIN_FACT")
		spec.authorityRef = nil
		if err := fixture.appendErr(spec); err == nil {
			t.Fatal("a domain fact without an authority reference was accepted")
		}
		spec.authorityRef = "authority:never-assigned"
		if err := fixture.appendErr(spec); err == nil {
			t.Fatal("a domain fact citing an unassigned authority was accepted")
		}
	})

	t.Run("a correction names its target", func(t *testing.T) {
		spec := fixture.event(2, "CORRECTION")
		spec.correctsStream = nil
		spec.correctsSequence = nil
		if err := fixture.appendErr(spec); err == nil {
			t.Fatal("a correction without a target was accepted")
		}
		spec = fixture.event(2, "CLAIM")
		spec.correctsStream = fixture.streamKey
		spec.correctsSequence = int64(1)
		if err := fixture.appendErr(spec); err == nil {
			t.Fatal("a non-correction carrying a correction target was accepted")
		}
	})

	t.Run("payload or artifact reference, never both and never neither", func(t *testing.T) {
		spec := fixture.event(3, "CLAIM")
		spec.payload = nil
		if err := fixture.appendErr(spec); err == nil {
			t.Fatal("an event with neither payload nor artifact reference was accepted")
		}
		spec = fixture.event(3, "CLAIM")
		spec.artifactRef = "artifact://sha256/" + fixtureDigestB
		if err := fixture.appendErr(spec); err == nil {
			t.Fatal("an event carrying both a payload and an artifact reference was accepted")
		}
		spec = fixture.event(3, "CLAIM")
		spec.payload = nil
		spec.artifactRef = "artifact://sha256/" + fixtureDigestB
		if err := fixture.appendErr(spec); err != nil {
			t.Fatalf("an artifact-referenced event was rejected: %v", err)
		}
	})

	t.Run("large bytes must become an artifact reference", func(t *testing.T) {
		spec := fixture.event(4, "CLAIM")
		spec.payload = make([]byte, 65537)
		if err := fixture.appendErr(spec); err == nil {
			t.Fatal("an oversized inline payload was accepted")
		}
		spec = fixture.event(4, "CLAIM")
		spec.payload = make([]byte, 65536)
		if err := fixture.appendErr(spec); err != nil {
			t.Fatalf("a payload at the inline limit was rejected: %v", err)
		}
	})
}

// TestTodo_LEDGER_001_Golden pins the assertion-class vocabulary and the three
// distinct event times.
func TestTodo_LEDGER_001_Golden(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := pgtest.New(t)

	var definition string
	if err := db.QueryRow(ctx, `
		SELECT pg_get_constraintdef(con.oid)
		FROM pg_constraint con
		JOIN pg_class c ON c.oid = con.conrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema()
		  AND c.relname = 'ledger_event'
		  AND con.conname = 'ledger_event_assertion_class_allowed'`).Scan(&definition); err != nil {
		t.Fatalf("read assertion class constraint: %v", err)
	}
	for _, class := range []string{
		"TRANSACTION_FACT", "DOMAIN_FACT", "EXTERNAL_OBSERVATION", "CLAIM", "CORRECTION",
	} {
		if !strings.Contains(definition, class) {
			t.Fatalf("assertion class constraint %q does not list %s", definition, class)
		}
	}

	for _, column := range []string{"occurred_at", "effective_at", "recorded_at"} {
		var dataType string
		if err := db.QueryRow(ctx, `
			SELECT data_type FROM information_schema.columns
			WHERE table_schema = $1 AND table_name = 'ledger_event' AND column_name = $2`,
			db.Schema, column).Scan(&dataType); err != nil {
			t.Fatalf("ledger_event has no %s column: %v", column, err)
		}
		if dataType != "timestamp with time zone" {
			t.Fatalf("ledger_event.%s is %s, want timestamp with time zone", column, dataType)
		}
	}
}
