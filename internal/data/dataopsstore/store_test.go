package dataopsstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops/importing"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func dataOpsFixture(t *testing.T, key string) (*Store, *pgtest.DB, uuid.UUID, importing.Batch, []byte, string) {
	t.Helper()
	db := pgtest.New(t)
	tenant := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, tenant, key, key)
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	payload := []byte("worker_id,name\nworker-1,Ada\n")
	h := sha256.Sum256(payload)
	sourceHash := "sha256:" + hex.EncodeToString(h[:])
	batch, err := importing.StageCSV(importing.SourceDescriptor{
		Kind: importing.SourceKindCSV, URI: "upload://" + key, Tenant: values.TenantId(key),
	}, values.NewInstant(fixedDataOpsTime()), bytesReader(payload))
	if err != nil {
		t.Fatalf("StageCSV: %v", err)
	}
	return New(conn), db, tenant, batch, payload, sourceHash
}

func fixedDataOpsTime() (t time.Time) { return time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC) }

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

// TestTodo_REV_030_01_Integration proves StageCSV's durable backing: the
// first request writes one immutable tenant-scoped artifact, a same-key replay
// returns that artifact, a different request is refused, and a foreign tenant
// cannot read it through the app role.
func TestTodo_REV_030_01_Integration(t *testing.T) {
	store, _, tenant, batch, payload, sourceHash := dataOpsFixture(t, "dataops-primary")
	ctx := context.Background()
	first, err := store.Put(ctx, tenant, "stage-1", sourceHash, batch, payload, "UNCLASSIFIED", "CSV")
	if err != nil {
		t.Fatalf("first Put: %v", err)
	}
	if first.BatchID == uuid.Nil || first.RowCount != 1 || first.SourceHash != sourceHash || string(first.Payload) != string(payload) {
		t.Fatalf("first receipt = %+v, want durable metadata and payload", first)
	}
	replay, err := store.Put(ctx, tenant, "stage-1", sourceHash, batch, payload, "UNCLASSIFIED", "CSV")
	if err != nil {
		t.Fatalf("replay Put: %v", err)
	}
	if replay.BatchID != first.BatchID {
		t.Fatalf("replay batch id = %s, want original %s", replay.BatchID, first.BatchID)
	}
	if _, err := store.Put(ctx, tenant, "stage-1", sourceHash, batch, append(payload, 'x'), "UNCLASSIFIED", "CSV"); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting replay = %v, want ErrConflict", err)
	}
	got, err := store.Get(ctx, tenant, first.BatchID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.BatchDigest != batch.Digest() || len(got.Header) != 2 || string(got.Payload) != string(payload) {
		t.Fatalf("Get = %+v, want original batch", got)
	}
	foreign := uuid.New()
	if _, err := store.Get(ctx, foreign, first.BatchID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign Get = %v, want ErrNotFound", err)
	}
}

// TestTodo_REV_030_01_Security proves the app role cannot mutate or bypass
// the append-only staging row and that the payload budget is rejected before a
// transaction is opened.
func TestTodo_REV_030_01_Security(t *testing.T) {
	store, _, tenant, batch, payload, sourceHash := dataOpsFixture(t, "dataops-security")
	ctx := context.Background()
	rec, err := store.Put(ctx, tenant, "stage-secure", sourceHash, batch, payload, "UNCLASSIFIED", "CSV")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := store.Put(ctx, tenant, "stage-large", sourceHash, batch, make([]byte, MaxPayloadBytes+1), "UNCLASSIFIED", "CSV"); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("oversized Put = %v, want ErrPayloadTooLarge", err)
	}
	// The append-only trigger and revoked UPDATE privilege are both checked by
	// executing through the same least-privilege connection the store uses.
	tx, err := store.db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin mutation transaction: %v", err)
	}
	if err := tenancy.WithTenant(ctx, tx, tenant); err != nil {
		t.Fatalf("scope mutation transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE dataops_import_batch SET classification='forged' WHERE tenant_id=$1 AND batch_id=$2`, tenant, rec.BatchID); err == nil {
		t.Fatal("app role updated immutable batch")
	}
	_ = tx.Rollback(ctx)
}
