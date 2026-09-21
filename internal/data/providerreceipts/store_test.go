package providerreceipts_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/providerreceipts"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func insertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, key, key)
	return id
}

// appConn is a connection running as the application role, so RLS and the
// SELECT/INSERT-only grant apply exactly as in production.
func appConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE hcmnext_app"); err != nil {
		t.Fatal(err)
	}
	return conn
}

// inTenant runs fn in a transaction scoped to tenant and commits it.
func inTenant(t *testing.T, conn dbport.Beginner, tenant uuid.UUID, fn func(tx dbport.Tx)) {
	t.Helper()
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatal(err)
	}
	fn(tx)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func receipt(tenant uuid.UUID, eventID, changeRef string, at time.Time) providerreceipts.Receipt {
	return providerreceipts.Receipt{
		TenantID:       tenant,
		Provider:       providerreceipts.ProviderPayroll,
		EventID:        eventID,
		EventType:      "payroll.change.applied",
		ChangeRef:      changeRef,
		CorrelationKey: "corr-1",
		Outcome:        providerreceipts.OutcomeApplied,
		ProviderRef:    "PSIM-1",
		Origin:         providerreceipts.OriginWebhook,
		Payload:        []byte(`{"event_id":"` + eventID + `","outcome":"APPLIED"}`),
		PayloadDigest:  "sha256:" + eventID,
		ReceivedAt:     at,
	}
}

var t0 = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

func TestRecordDuplicateConflictAndLatest(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "receipts-a")
	conn := appConn(t, db)
	var store providerreceipts.Store
	ctx := context.Background()
	signal := uuid.New()

	inTenant(t, conn, tenant, func(tx dbport.Tx) {
		first := receipt(tenant, "evt-1", "payroll:rev-1", t0)
		first.SignalID = &signal
		dup, err := store.Record(ctx, tx, first)
		if err != nil || dup {
			t.Fatalf("first record = %v, %v", dup, err)
		}
		dup, err = store.Record(ctx, tx, first)
		if err != nil || !dup {
			t.Fatalf("identical re-record = %v, %v; want duplicate", dup, err)
		}
		changed := first
		changed.PayloadDigest = "sha256:other"
		changed.Reason = "restated"
		if _, err := store.Record(ctx, tx, changed); !errors.Is(err, providerreceipts.ErrConflict) {
			t.Fatalf("different digest err = %v, want ErrConflict", err)
		}
	})

	inTenant(t, conn, tenant, func(tx dbport.Tx) {
		later := receipt(tenant, "evt-0-later", "payroll:rev-1", t0.Add(time.Minute))
		later.Outcome = providerreceipts.OutcomeRejected
		later.EventType = "payroll.change.rejected"
		later.Reason = "pay above band"
		if _, err := store.Record(ctx, tx, later); err != nil {
			t.Fatal(err)
		}
		got, found, err := store.LatestForChange(ctx, tx, tenant, "payroll:rev-1")
		if err != nil || !found {
			t.Fatalf("latest = %v, %v", found, err)
		}
		if got.EventID != "evt-0-later" || got.Outcome != "REJECTED" || got.Reason != "pay above band" || !got.ReceivedAt.Equal(t0.Add(time.Minute)) || got.SignalID != nil {
			t.Fatalf("latest = %+v", got)
		}
		if !strings.Contains(string(got.Payload), `"event_id"`) {
			t.Fatalf("payload = %s", got.Payload)
		}
		if _, found, err := store.LatestForChange(ctx, tx, tenant, "payroll:none"); err != nil || found {
			t.Fatalf("absent change = %v, %v", found, err)
		}
	})

	// The original row survived the conflicting restatement untouched, and
	// its nullable signal id round-trips.
	inTenant(t, conn, tenant, func(tx dbport.Tx) {
		var digest string
		var sig uuid.NullUUID
		if err := tx.QueryRow(ctx, `SELECT payload_digest, signal_id FROM integration_provider_receipt WHERE event_id='evt-1'`).Scan(&digest, &sig); err != nil {
			t.Fatal(err)
		}
		if digest != "sha256:evt-1" || !sig.Valid || sig.UUID != signal {
			t.Fatalf("stored = %s %+v", digest, sig)
		}
	})
}

func TestTenantIsolation(t *testing.T) {
	db := pgtest.New(t)
	a := insertTenant(t, db, "receipts-iso-a")
	b := insertTenant(t, db, "receipts-iso-b")
	conn := appConn(t, db)
	var store providerreceipts.Store
	ctx := context.Background()
	inTenant(t, conn, a, func(tx dbport.Tx) {
		if _, err := store.Record(ctx, tx, receipt(a, "evt-a", "payroll:shared", t0)); err != nil {
			t.Fatal(err)
		}
	})
	inTenant(t, conn, b, func(tx dbport.Tx) {
		if _, found, err := store.LatestForChange(ctx, tx, b, "payroll:shared"); err != nil || found {
			t.Fatalf("tenant b saw tenant a's receipt: %v %v", found, err)
		}
		if _, found, err := store.LatestForChange(ctx, tx, a, "payroll:shared"); err != nil || found {
			t.Fatalf("tenant b read tenant a's receipt by id: %v %v", found, err)
		}
	})
	// Writing a receipt for tenant a from a tenant-b scope is refused by RLS.
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, b); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(ctx, tx, receipt(a, "evt-cross", "payroll:x", t0)); err == nil {
		t.Fatal("cross-tenant insert was admitted")
	}
}

func TestAppendOnly(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "receipts-ao")
	var store providerreceipts.Store
	ctx := context.Background()
	inTenant(t, appConn(t, db), tenant, func(tx dbport.Tx) {
		if _, err := store.Record(ctx, tx, receipt(tenant, "evt-ao", "payroll:ao", t0)); err != nil {
			t.Fatal(err)
		}
	})
	// The owning connection holds UPDATE/DELETE privileges, so only the
	// forbid_mutation trigger stands between it and the row.
	for _, stmt := range []string{
		`UPDATE integration_provider_receipt SET reason = 'edited' WHERE event_id = 'evt-ao'`,
		`DELETE FROM integration_provider_receipt WHERE event_id = 'evt-ao'`,
	} {
		tx, err := db.Conn.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, stmt)
		_ = tx.Rollback(ctx)
		if err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("%s: err = %v, want forbid_mutation refusal", stmt, err)
		}
	}
	// The application role is not even granted UPDATE.
	tx, err := appConn(t, db).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE integration_provider_receipt SET reason = 'x'`); err == nil {
		t.Fatal("application role updated a receipt")
	}
}

func TestSchemaChecksBackstopValidation(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "receipts-chk")
	err := db.ExecErr(`INSERT INTO integration_provider_receipt (tenant_id, provider, event_id, event_type, change_ref, correlation_key, outcome, origin, payload, payload_digest, received_at)
VALUES ($1, 'workday', 'e', 't', 'c', 'k', 'APPLIED', 'webhook', '{}', 'd', now())`, tenant)
	if err == nil {
		t.Fatal("unknown provider admitted by schema")
	}
}

func TestUndoOutcomesStatusPollAndLatestByKind(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "receipts-kind")
	conn := appConn(t, db)
	var store providerreceipts.Store
	ctx := context.Background()
	settle := []string{providerreceipts.OutcomeApplied, providerreceipts.OutcomeGranted, providerreceipts.OutcomeRejected}
	undo := []string{providerreceipts.OutcomeReversed, providerreceipts.OutcomeRevoked}

	inTenant(t, conn, tenant, func(tx dbport.Tx) {
		idx := 1
		applied := receipt(tenant, "evt-apply", "payroll:k-1", t0)
		applied.SecretIndex = &idx
		if _, err := store.Record(ctx, tx, applied); err != nil {
			t.Fatal(err)
		}
		reversed := receipt(tenant, "evt-reverse", "payroll:k-1", t0.Add(time.Minute))
		reversed.Outcome, reversed.EventType = providerreceipts.OutcomeReversed, "payroll.change.reversed"
		if _, err := store.Record(ctx, tx, reversed); err != nil {
			t.Fatalf("REVERSED record: %v", err)
		}
		polled := receipt(tenant, "poll-1", "iam:k-2", t0)
		polled.Provider, polled.Outcome, polled.EventType, polled.Origin = providerreceipts.ProviderIAM, providerreceipts.OutcomeRevoked, "status.REVOKED", providerreceipts.OriginStatusPoll
		if _, err := store.Record(ctx, tx, polled); err != nil {
			t.Fatalf("status_poll REVOKED record: %v", err)
		}

		// The undo is the latest receipt overall...
		if got, found, err := store.LatestForChange(ctx, tx, tenant, "payroll:k-1"); err != nil || !found || got.Outcome != providerreceipts.OutcomeReversed {
			t.Fatalf("latest = %+v %v %v", got, found, err)
		}
		// ...but it does not hide the apply from a settle-kind question.
		got, found, err := store.LatestForChangeByKind(ctx, tx, tenant, "payroll:k-1", settle...)
		if err != nil || !found || got.EventID != "evt-apply" || got.Outcome != providerreceipts.OutcomeApplied || got.SecretIndex == nil || *got.SecretIndex != 1 {
			t.Fatalf("latest settle = %+v %v %v", got, found, err)
		}
		got, found, err = store.LatestForChangeByKind(ctx, tx, tenant, "payroll:k-1", undo...)
		if err != nil || !found || got.EventID != "evt-reverse" || got.SecretIndex != nil {
			t.Fatalf("latest undo = %+v %v %v", got, found, err)
		}
		if _, found, err := store.LatestForChangeByKind(ctx, tx, tenant, "payroll:k-1", providerreceipts.OutcomeRejected); err != nil || found {
			t.Fatalf("rejected-only = %v %v", found, err)
		}
		got, found, err = store.LatestForChangeByKind(ctx, tx, tenant, "iam:k-2", undo...)
		if err != nil || !found || got.Origin != providerreceipts.OriginStatusPoll || got.Outcome != providerreceipts.OutcomeRevoked {
			t.Fatalf("polled undo = %+v %v %v", got, found, err)
		}
		if _, found, err := store.LatestForChangeByKind(ctx, tx, tenant, "iam:k-2", settle...); err != nil || found {
			t.Fatalf("polled settle = %v %v", found, err)
		}
	})

	// Tenant isolation holds for the by-kind read too.
	other := insertTenant(t, db, "receipts-kind-b")
	inTenant(t, conn, other, func(tx dbport.Tx) {
		if _, found, err := store.LatestForChangeByKind(ctx, tx, tenant, "payroll:k-1", settle...); err != nil || found {
			t.Fatalf("cross-tenant by-kind read = %v %v", found, err)
		}
	})
}

func TestSchemaChecksBackstopNewColumns(t *testing.T) {
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "receipts-chk2")
	insert := func(outcome, origin, secretIndex string) error {
		return db.ExecErr(`INSERT INTO integration_provider_receipt (tenant_id, provider, event_id, event_type, change_ref, correlation_key, outcome, origin, payload, payload_digest, received_at, secret_index)
VALUES ($1, 'payroll', gen_random_uuid()::text, 't', 'c', 'k', $2, $3, '{}', 'd', now(), `+secretIndex+`)`, tenant, outcome, origin)
	}
	for _, ok := range [][3]string{{"REVERSED", "webhook", "0"}, {"REVOKED", "status_poll", "NULL"}, {"APPLIED", "webhook", "3"}} {
		if err := insert(ok[0], ok[1], ok[2]); err != nil {
			t.Fatalf("%v refused: %v", ok, err)
		}
	}
	for _, bad := range [][3]string{{"UNDONE", "webhook", "NULL"}, {"APPLIED", "email", "NULL"}, {"APPLIED", "status_poll", "0"}, {"APPLIED", "webhook", "-1"}} {
		if err := insert(bad[0], bad[1], bad[2]); err == nil {
			t.Fatalf("%v admitted by schema", bad)
		}
	}
}
