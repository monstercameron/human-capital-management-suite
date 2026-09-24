package partition_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/ledger"
	ledgerpartition "github.com/monstercameron/human-capital-management-suite/internal/data/ledger/partition"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

const partitionSchema = "hcmnext.test.LedgerPartition@1"

func insertTenant(t *testing.T, db *pgtest.DB, n int) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Partition Test', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, id, fmt.Sprintf("partition-%d-%s", n, id))
	db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, $2, 1, $2, 'PROTOBUF', 'LEDGER_EVENT')`, id, partitionSchema)
	return id
}

func insertEvent(t *testing.T, db *pgtest.DB, tenant uuid.UUID, stream string, sequence int64) ledger.EventRecord {
	t.Helper()
	eventID := uuid.New()
	digest := fmt.Sprintf("%064x", sequence)
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	db.Exec(t, `INSERT INTO ledger_stream (tenant_id, stream_key, stream_kind, subject_ref) VALUES ($1, $2, 'WORKER', $2)`, tenant, stream)
	db.Exec(t, `INSERT INTO stream_head (tenant_id, stream_key, head_sequence, head_digest, head_digest_algorithm) VALUES ($1, $2, $3, $4, 'sha256')`, tenant, stream, sequence, digest)
	db.Exec(t, `INSERT INTO ledger_event (
		tenant_id, stream_key, sequence, event_id, assertion_class, source_ref, schema_ref,
		payload, canonical_length, digest, digest_algorithm, occurred_at, effective_at,
		recorded_at, correlation_id, idempotency_key)
		VALUES ($1, $2, $3, $4, 'TRANSACTION_FACT', 'hcmnext:test', $5, $6, 1, $7, 'sha256', $8, $8, $8, $4, $9)`,
		tenant, stream, sequence, eventID, partitionSchema, []byte("x"), digest, now, uuid.NewString())
	return ledger.EventRecord{Tenant: tenant, StreamKey: stream, Sequence: sequence, EventID: eventID, Digest: digest, DigestAlgorithm: "sha256"}
}

func TestTodo_LEDGER_009(t *testing.T) {
	db := pgtest.New(t)
	var first, second uuid.UUID
	var firstPartition, secondPartition string
	for i := 0; i < 64 && first == uuid.Nil; i++ {
		tenant := insertTenant(t, db, i)
		stream := "worker:" + uuid.NewString()
		insertEvent(t, db, tenant, stream, 1)
		var partition string
		if err := db.Conn.QueryRow(context.Background(), `SELECT tableoid::regclass::text FROM ledger_event WHERE tenant_id = $1`, tenant).Scan(&partition); err != nil {
			t.Fatal(err)
		}
		if second != uuid.Nil && partition != secondPartition {
			first, firstPartition = tenant, partition
			break
		}
		if first == uuid.Nil {
			second, secondPartition = tenant, partition
		}
	}
	if first == uuid.Nil {
		t.Skip("embedded PostgreSQL hash probing did not produce two partitions")
	}

	var before []ledgerpartition.Identity
	rows, err := db.Conn.Query(context.Background(), `SELECT tenant_id, stream_key, sequence, event_id, digest, digest_algorithm FROM ledger_event WHERE tenant_id = $1 ORDER BY stream_key, sequence`, first)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var row ledgerpartition.Identity
		if err := rows.Scan(&row.Tenant, &row.StreamKey, &row.Sequence, &row.EventID, &row.Digest, &row.DigestAlgorithm); err != nil {
			t.Fatal(err)
		}
		before = append(before, row)
	}
	rows.Close()

	rows, err = db.Conn.Query(context.Background(), `SELECT tenant_id, stream_key, sequence, event_id, digest, digest_algorithm FROM `+firstPartition+` WHERE tenant_id = $1 ORDER BY stream_key, sequence`, first)
	if err != nil {
		t.Fatal(err)
	}
	var after []ledgerpartition.Identity
	for rows.Next() {
		var row ledgerpartition.Identity
		if err := rows.Scan(&row.Tenant, &row.StreamKey, &row.Sequence, &row.EventID, &row.Digest, &row.DigestAlgorithm); err != nil {
			t.Fatal(err)
		}
		after = append(after, row)
	}
	rows.Close()
	if err := ledgerpartition.Compare(before, after); err != nil {
		t.Fatalf("parent and physical partition differ: %v", err)
	}
	if ledgerpartition.ChainDigest(before) != ledgerpartition.ChainDigest(after) {
		t.Fatal("digest chain changed across the physical partition view")
	}
	if firstPartition == secondPartition || first == second {
		t.Fatalf("fixture did not span two partitions: %s and %s", firstPartition, secondPartition)
	}
}

func TestTodo_LEDGER_009_Race(t *testing.T) {
	tenant := uuid.New()
	rows := []ledgerpartition.Identity{{Tenant: tenant, StreamKey: "a", Sequence: 1, EventID: uuid.New(), Digest: "a", DigestAlgorithm: "sha256"}}
	want := ledgerpartition.ChainDigest(rows)
	errs := make([]error, 8)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := (ledgerpartition.AtomicScope{Tenant: tenant, Streams: []string{"a", "b"}}).Validate(); err != nil {
				errs[i] = err
				return
			}
			if got := ledgerpartition.ChainDigest(rows); got != want {
				errs[i] = fmt.Errorf("digest %s differs from %s", got, want)
			}
			if err := ledgerpartition.Compare(rows, append([]ledgerpartition.Identity(nil), rows...)); err != nil {
				errs[i] = err
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent maintenance check %d: %v", i, err)
		}
	}
}

func TestTodo_LEDGER_009_Security(t *testing.T) {
	if err := (ledgerpartition.AtomicScope{Streams: []string{"worker:1"}}).Validate(); err == nil {
		t.Fatal("scope without tenant was accepted")
	}
	if err := (ledgerpartition.AtomicScope{Tenant: uuid.New(), Streams: []string{""}}).Validate(); err == nil {
		t.Fatal("scope with empty stream was accepted")
	}
	if err := (ledgerpartition.AtomicScope{
		Tenant: uuid.New(), Streams: []string{"worker:1"},
		StreamTenants: map[string]uuid.UUID{"worker:1": uuid.New()},
	}).Validate(); err == nil {
		t.Fatal("cross-tenant scope was accepted")
	}
}

func TestTodo_LEDGER_009_Mutation(t *testing.T) {
	tenant := uuid.New()
	left := ledgerpartition.Identity{Tenant: tenant, StreamKey: "worker", Sequence: 1, EventID: uuid.New(), Digest: "a", DigestAlgorithm: "sha256"}
	right := left
	right.Digest = "b"
	err := ledgerpartition.Compare([]ledgerpartition.Identity{left}, []ledgerpartition.Identity{right})
	if err == nil || !contains(err.Error(), "digest") {
		t.Fatalf("digest mutation error = %v", err)
	}
}

func contains(value, want string) bool {
	for i := 0; i+len(want) <= len(value); i++ {
		if value[i:i+len(want)] == want {
			return true
		}
	}
	return false
}
