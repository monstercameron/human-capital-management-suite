package webhookreceipts

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/providerreceipt"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/webhook"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func webhookTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

func webhookAppConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE hcmnext_app"); err != nil {
		t.Fatal(err)
	}
	return conn
}

func webhookTenantTx(t *testing.T, conn dbport.Beginner, tenant uuid.UUID, fn func(dbport.Tx)) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatal(err)
	}
	fn(tx)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestTodo_INTG_018_DurableInboxIntegration(t *testing.T) {
	db := pgtest.New(t)
	tenant := webhookTenant(t, db, "webhook-receipt-a")
	otherTenant := webhookTenant(t, db, "webhook-receipt-b")
	conn := webhookAppConn(t, db)
	scope := Scope{TenantID: tenant, Provider: "payroll", EndpointID: "payroll-ep-a"}
	store, err := New(conn, scope)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 24, 15, 0, 0, 0, time.UTC)
	raw := []byte("{ \"event_id\": \"event-durable\", \"outcome\": \"APPLIED\" }\n")
	parsed := providerreceipt.Parsed{
		Provider: "payroll", EventID: "event-durable", EventType: providerreceipt.PayrollEventApplied,
		Schema: providerreceipt.PayrollSchema, TenantID: tenant.String(), ChangeRef: "payroll:change-durable",
		CorrelationKey: "correlation-durable", ProviderRef: "provider-durable", Outcome: providerreceipt.OutcomeApplied,
		Payload: raw, PayloadDigest: digest(raw), Details: map[string]string{"currency": "USD"},
	}
	if duplicate, err := store.Record(context.Background(), parsed, now); err != nil || duplicate {
		t.Fatalf("first record duplicate=%v err=%v", duplicate, err)
	}
	if duplicate, err := store.Record(context.Background(), parsed, now.Add(time.Second)); err != nil || !duplicate {
		t.Fatalf("identical redelivery duplicate=%v err=%v", duplicate, err)
	}
	changed := parsed
	changed.EventType = providerreceipt.PayrollEventRejected
	if _, err := store.Record(context.Background(), changed, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("same event with changed envelope error=%v", err)
	}

	// A newly composed store sees the same receipt and outbox after restart.
	restarted, err := New(conn, scope)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := restarted.Replay(context.Background(), parsed.EventID, webhook.ReplayApproval{Actor: "operator-1", Purpose: "recovery", Approved: true})
	if err != nil || string(replayed.Payload) != string(raw) || replayed.PayloadDigest != parsed.PayloadDigest || replayed.Details["currency"] != "USD" {
		t.Fatalf("replay parsed=%+v err=%v", replayed, err)
	}

	webhookTenantTx(t, conn, tenant, func(tx dbport.Tx) {
		var receipts, pending int
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM integration_webhook_receipt WHERE tenant_id=$1 AND event_id=$2`, tenant, parsed.EventID).Scan(&receipts); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow(context.Background(), `SELECT count(*) FROM integration_webhook_outbox WHERE tenant_id=$1 AND receipt_id=$2 AND status='PENDING'`, tenant, receiptUUID(scope, parsed.EventID)).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if receipts != 1 || pending != 1 {
			t.Fatalf("receipt/outbox rows = %d/%d, want 1/1", receipts, pending)
		}
	})

	// RLS hides tenant A's receipt from tenant B while allowing an independent
	// event with the same provider event ID in B's own scope.
	otherScope := scope
	otherScope.TenantID = otherTenant
	otherStore, err := New(conn, otherScope)
	if err != nil {
		t.Fatal(err)
	}
	approval := webhook.ReplayApproval{Actor: "operator-1", Purpose: "recovery", Approved: true}
	if _, err := otherStore.Replay(context.Background(), parsed.EventID, approval); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other tenant read tenant A's receipt: %v", err)
	}
	otherParsed := parsed
	otherParsed.TenantID = otherTenant.String()
	otherParsed.ProviderRef = "provider-b"
	if duplicate, err := otherStore.Record(context.Background(), otherParsed, now); err != nil || duplicate {
		t.Fatalf("other tenant insert duplicate=%v err=%v", duplicate, err)
	}
	otherReplay, err := otherStore.Replay(context.Background(), parsed.EventID, approval)
	if err != nil || otherReplay.ProviderRef != "provider-b" {
		t.Fatalf("other tenant replay=%+v err=%v", otherReplay, err)
	}
	ownerConn := db.NewConn(t)
	if _, err := ownerConn.Exec(context.Background(), `UPDATE integration_webhook_receipt SET event_type='tampered' WHERE tenant_id=$1 AND event_id=$2`, tenant, parsed.EventID); err == nil {
		t.Fatal("append-only receipt update succeeded")
	}
}
