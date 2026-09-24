package main

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
)

// TestTodo_WF_REV_013_Integration proves the production leased worker holds
// a real PostgreSQL outbox compensation until the original row is delivered,
// then dispatches and acknowledges it exactly once.
func TestTodo_WF_REV_013_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := uuid.New()
	ctx := context.Background()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', 'Acme', 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenant, "acme-"+tenant.String())
	const schemaRef = "hcmnext.test.v1.WFREV013@1"
	db.Exec(t, `
		INSERT INTO payload_schema (
			tenant_id, schema_ref, schema_id, schema_version,
			message_full_name, wire_format, canonicalization_profile)
		VALUES ($1, $2, 'hcmnext.test.v1.WFREV013', 1,
			'hcmnext.test.v1.WFREV013', 'PROTOBUF', 'LEDGER_EVENT')`, tenant, schemaRef)

	originalEffect := "payroll:wf-rev-013-worker"
	compensationEffect := outbox.CompensationPrefix + originalEffect
	enqueue := func(effect string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		tx, err := db.Conn.Begin(ctx)
		if err != nil {
			t.Fatalf("begin enqueue %s: %v", effect, err)
		}
		if _, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{
			Tenant: tenant, OutboxID: id, EffectIdentity: effect, OrderingKey: "wf-rev-013-worker",
			SchemaRef: schemaRef, Payload: []byte("effect:" + effect),
		}); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatalf("enqueue %s: %v", effect, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit enqueue %s: %v", effect, err)
		}
		return id
	}
	compensationID := enqueue(compensationEffect)
	// Separate transaction timestamps so the first real Poll returns the
	// compensation before its original.
	time.Sleep(5 * time.Millisecond)
	originalID := enqueue(originalEffect)

	pool := schemaScopedPool(t, db)
	consumer := outbox.NewConsumer(pool, outbox.WithBatchSize(2))
	var applied []string
	handler := func(_ context.Context, msg outbox.Record) error {
		applied = append(applied, msg.EffectIdentity)
		return nil
	}
	logger := discardLogger()
	tenants := fakeTenantLister{tenants: []uuid.UUID{tenant}}

	didWork, err := sweepWithTelemetry(ctx, logger, tenants, consumer, handler, nil, time.Now)
	if err != nil || !didWork {
		t.Fatalf("first sweep = %t, %v; want work", didWork, err)
	}
	if len(applied) != 1 || applied[0] != originalEffect {
		t.Fatalf("first-sweep handler calls = %v, want only the original", applied)
	}
	var status string
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT status, attempts FROM outbox WHERE tenant_id=$1 AND outbox_id=$2`, tenant, compensationID).Scan(&status, &attempts); err != nil {
		t.Fatalf("read held compensation: %v", err)
	}
	if status != outbox.StatusPending || attempts != 0 {
		t.Fatalf("held compensation status=%s attempts=%d; want PENDING and no consumed attempt", status, attempts)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM outbox WHERE tenant_id=$1 AND outbox_id=$2`, tenant, originalID).Scan(&status); err != nil || status != outbox.StatusDelivered {
		t.Fatalf("original status=%q err=%v; want DELIVERED", status, err)
	}

	time.Sleep(time.Second + 25*time.Millisecond)
	didWork, err = sweepWithTelemetry(ctx, logger, tenants, consumer, handler, nil, time.Now)
	if err != nil || !didWork {
		t.Fatalf("second sweep = %t, %v; want deferred compensation work", didWork, err)
	}
	if len(applied) != 2 || applied[0] != originalEffect || applied[1] != compensationEffect {
		t.Fatalf("handler calls = %v, want original then compensation exactly once", applied)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM outbox WHERE tenant_id=$1 AND outbox_id=$2`, tenant, compensationID).Scan(&status); err != nil || status != outbox.StatusDelivered {
		t.Fatalf("compensation status=%q err=%v; want DELIVERED", status, err)
	}
	if _, err := sweepWithTelemetry(ctx, logger, tenants, consumer, handler, nil, time.Now); err != nil {
		t.Fatalf("redelivery sweep: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("handler calls after redelivery sweep = %v, want no duplicate effect", applied)
	}
}
