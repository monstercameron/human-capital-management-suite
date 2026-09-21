package partition_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

const pSchemaRef = "hcmnext.intents.v1.BusinessIntent@1"

// insertTenant registers one active tenant, as the admin role, and returns
// its identifier.
func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

// insertLedgerPrereqs stands up the payload schema, authority assignment,
// ledger stream and stream head one tenant's stream needs before any
// ledger_event row can reference it.
func insertLedgerPrereqs(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, streamKey string) (schemaRef, authorityRef string) {
	t.Helper()
	schemaRef = pSchemaRef
	authorityRef = "authority:" + streamKey

	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.intents.v1.BusinessIntent', 1,
			'hcmnext.intents.v1.BusinessIntent', 'PROTOBUF', 'LEDGER_EVENT')
		ON CONFLICT DO NOTHING`,
		tenantID, schemaRef)

	db.Exec(t, `
		INSERT INTO authority_assignment (tenant_id, authority_ref, authority_kind, domain_scope, effective_from)
		VALUES ($1, $2, 'INTERNAL', 'workforce.compensation', timestamptz '2026-01-01T00:00:00Z')
		ON CONFLICT DO NOTHING`,
		tenantID, authorityRef)

	db.Exec(t, `
		INSERT INTO ledger_stream (tenant_id, stream_key, stream_kind, subject_ref)
		VALUES ($1, $2, 'WORKER', $2)`, tenantID, streamKey)

	db.Exec(t, `
		INSERT INTO stream_head (tenant_id, stream_key, head_sequence)
		VALUES ($1, $2, 0)`, tenantID, streamKey)

	return schemaRef, authorityRef
}

// insertLedgerEvent appends one ledger_event row for tenantID's streamKey at
// sequence, as the admin role. Prerequisites (schema/authority/stream/head)
// must already exist; insertLedgerPrereqs sets them up once per stream.
func insertLedgerEvent(t *testing.T, db *pgtest.DB, tenantID uuid.UUID, streamKey, schemaRef, authorityRef string, sequence int, idempotencyKey string) uuid.UUID {
	t.Helper()
	eventID := uuid.New()
	db.Exec(t, `
		INSERT INTO ledger_event (
			tenant_id, stream_key, sequence, event_id, assertion_class, authority_ref,
			source_ref, schema_ref, payload, canonical_length, digest, digest_algorithm,
			occurred_at, effective_at, correlation_id, idempotency_key)
		VALUES ($1, $2, $3, $4, 'DOMAIN_FACT', $5, 'test', $6, $7, $8,
			'1111111111111111111111111111111111111111111111111111111111111111', 'sha256',
			timestamptz '2026-02-01T00:00:00Z', timestamptz '2026-02-01T00:00:00Z', $4, $9)`,
		tenantID, streamKey, sequence, eventID, authorityRef, schemaRef,
		[]byte("body"), len("body"), idempotencyKey)
	return eventID
}

// appRoleConn opens a fresh connection on db's schema and assumes the
// hcmnext_app role on it, exactly as a production bootstrap connection
// would (internal/data/tenancy's own fixtures use the identical pattern).
func appRoleConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

// scopedTx begins a transaction on conn and scopes it to tenantID.
func scopedTx(t *testing.T, ctx context.Context, conn *pgxadapter.Conn, tenantID uuid.UUID) dbport.Tx {
	t.Helper()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("scope transaction to tenant %s: %v", tenantID, err)
	}
	return tx
}

// pickTwoTenantsInDifferentPartitions registers probe tenants (each with one
// minimal, real ledger_event row) until two of them land in different
// physical ledger_event partitions -- observed via [partitionOf], never
// assumed from a hash formula this package would have to keep in sync with
// PostgreSQL's own. With four partitions this converges in a handful of
// tries; the cap exists only to fail loudly instead of looping forever if
// ledger_event were ever repartitioned down to one partition.
func pickTwoTenantsInDifferentPartitions(t *testing.T, ctx context.Context, db *pgtest.DB) (tenantA, tenantB uuid.UUID) {
	t.Helper()
	byPartition := map[string]uuid.UUID{}
	for i := 0; i < 40; i++ {
		tid := insertTenant(t, db, fmt.Sprintf("probe-%d", i))
		streamKey := fmt.Sprintf("probe-stream-%d", i)
		schemaRef, authorityRef := insertLedgerPrereqs(t, db, tid, streamKey)
		insertLedgerEvent(t, db, tid, streamKey, schemaRef, authorityRef, 1, fmt.Sprintf("probe-idem-%d", i))

		part := partitionOf(t, ctx, db.Conn, tid)
		for otherPart, existing := range byPartition {
			if otherPart != part {
				return existing, tid
			}
		}
		byPartition[part] = tid
	}
	t.Fatal("pickTwoTenantsInDifferentPartitions: no two of 40 probe tenants landed in different ledger_event partitions")
	return uuid.Nil, uuid.Nil
}

// tableOID returns which physical partition (or, for a non-partitioned
// table, the table itself) a specific tenant's rows land in, via the
// tableoid system column -- the same mechanism PostgreSQL itself uses to
// answer "which relation did this row actually come from".
func partitionOf(t *testing.T, ctx context.Context, admin dbport.Conn, tenantID uuid.UUID) string {
	t.Helper()
	var relname string
	if err := admin.QueryRow(ctx, `
		SELECT DISTINCT tableoid::regclass::text
		FROM ledger_event WHERE tenant_id = $1`, tenantID).Scan(&relname); err != nil {
		t.Fatalf("determine partition for tenant %s: %v", tenantID, err)
	}
	return relname
}
