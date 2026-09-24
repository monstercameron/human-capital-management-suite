package schema_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestTodo_DATA_001_Integration exercises the tenant boundary through the
// migrated PostgreSQL schema, including a real event write and a forged
// cross-tenant reference to its stream.
func TestTodo_DATA_001_Integration(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	owner := newLedgerFixture(t, db)
	foreignTenant := insertNamedTenant(t, db, "foreign-tenant")

	// A matching schema in the second tenant ensures that rejection below is
	// caused by the stream ownership boundary, not by an unrelated schema FK.
	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.BusinessIntent', 1,
			'hcmnext.intents.v1.BusinessIntent', 'PROTOBUF', 'LEDGER_EVENT')`,
		foreignTenant, owner.schemaRef)

	owner.appendEvent(t, 1, "TRANSACTION_FACT")
	var persistedTenant uuid.UUID
	var persistedStream string
	var persistedSequence int64
	if err := db.QueryRow(context.Background(), `
		SELECT tenant_id, stream_key, sequence FROM ledger_event
		WHERE tenant_id = $1 AND stream_key = $2 AND sequence = 1`,
		owner.tenant, owner.streamKey).Scan(&persistedTenant, &persistedStream, &persistedSequence); err != nil {
		t.Fatalf("read committed event through migrated schema: %v", err)
	}
	if persistedTenant != owner.tenant || persistedStream != owner.streamKey || persistedSequence != 1 {
		t.Fatalf("persisted event identity is %s/%s/%d, want %s/%s/1",
			persistedTenant, persistedStream, persistedSequence, owner.tenant, owner.streamKey)
	}

	// Reuse valid values for every required field while borrowing the other
	// tenant's identity and the owner's stream. The composite FK must refuse it.
	err := db.ExecErr(`
		INSERT INTO ledger_event (
			tenant_id, stream_key, sequence, event_id, assertion_class, source_ref,
			schema_ref, payload, canonical_length, digest, digest_algorithm,
			occurred_at, effective_at, correlation_id, idempotency_key)
		VALUES ($1, $2, 2, $3, 'TRANSACTION_FACT', 'integration-test', $4,
			'forged', 6, $5, 'sha256', $6, $6, $7, 'cross-tenant-forgery')`,
		foreignTenant, owner.streamKey, uuid.New(), owner.schemaRef, fixtureDigestA,
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), uuid.New())
	if err == nil {
		t.Fatal("migrated schema accepted an event whose tenant does not own its stream")
	}
	var rows int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, foreignTenant).Scan(&rows); err != nil {
		t.Fatalf("count forged tenant events: %v", err)
	}
	if rows != 0 {
		t.Fatalf("foreign tenant has %d event rows after rejected cross-tenant write, want 0", rows)
	}
}

// TestTodo_DATA_001_Mutation targets a common schema mutant: dropping tenant_id
// from the event-to-stream foreign key would let one tenant borrow another's
// authoritative stream identity.
func TestTodo_DATA_001_Mutation(t *testing.T) {
	t.Parallel()
	db := pgtest.New(t)
	owner := newLedgerFixture(t, db)
	foreignTenant := insertNamedTenant(t, db, "mutant-tenant")
	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.BusinessIntent', 1,
			'hcmnext.intents.v1.BusinessIntent', 'PROTOBUF', 'LEDGER_EVENT')`,
		foreignTenant, owner.schemaRef)

	if err := db.ExecErr(`
		INSERT INTO ledger_event (
			tenant_id, stream_key, sequence, event_id, assertion_class, source_ref,
			schema_ref, payload, canonical_length, digest, digest_algorithm,
			occurred_at, effective_at, correlation_id, idempotency_key)
		VALUES ($1, $2, 1, $3, 'TRANSACTION_FACT', 'mutation-test', $4,
			'body', 4, $5, 'sha256', timestamptz '2026-02-01T00:00:00Z',
			timestamptz '2026-02-01T00:00:00Z', $6, 'borrowed-stream')`,
		foreignTenant, owner.streamKey, uuid.New(), owner.schemaRef, fixtureDigestA, uuid.New()); err == nil {
		t.Fatal("cross-tenant event insert succeeded; the tenant component of the stream FK is load-bearing")
	}

	var count int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, foreignTenant).Scan(&count); err != nil {
		t.Fatalf("count tenant events after refused mutation: %v", err)
	}
	if count != 0 {
		t.Fatalf("tenant has %d events after refused write, want 0", count)
	}
}
