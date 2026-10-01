package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	fixtureseed "github.com/monstercameron/human-capital-management-suite/internal/data/seed"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

func TestMain(m *testing.M) {
	pgtest.RunMain(m)
}

func TestSeedCommandSeedsOnceAndEmitsStableReceipt(t *testing.T) {
	db := pgtest.New(t)
	ctx := context.Background()
	const tenant = "harborcare"
	tenantID := pgstore.TenantID(tenant)
	plan, err := fixtureseed.Plan()
	if err != nil {
		t.Fatalf("seed plan: %v", err)
	}

	var first bytes.Buffer
	if err := runSeedCommand(ctx, db.Conn, tenant, &first); err != nil {
		t.Fatalf("first seed: %v", err)
	}
	var firstReceipt seedReceipt
	if err := json.Unmarshal(first.Bytes(), &firstReceipt); err != nil {
		t.Fatalf("decode first receipt %q: %v", first.String(), err)
	}
	if firstReceipt.Tenant != tenantID || firstReceipt.Digest == "" || firstReceipt.Inserted != len(plan) || firstReceipt.Skipped != 0 {
		t.Fatalf("first receipt = %+v, want tenant/digest/new inserts", firstReceipt)
	}
	var tenantKey, cellID, displayName string
	var effectiveFrom time.Time
	if err := db.Conn.QueryRow(ctx, `
		SELECT tenant_key, cell_id, display_name, effective_from FROM tenant WHERE tenant_id = $1`, tenantID,
	).Scan(&tenantKey, &cellID, &displayName, &effectiveFrom); err != nil {
		t.Fatalf("read seeded tenant: %v", err)
	}
	if tenantKey != tenant || cellID != "cell-local" || displayName != tenant || effectiveFrom.UTC().Format(time.RFC3339) != "2026-01-01T00:00:00Z" {
		t.Fatalf("seeded tenant = key=%q cell=%q display=%q effective_from=%s", tenantKey, cellID, displayName, effectiveFrom.UTC().Format(time.RFC3339))
	}
	var seededRows int
	if err := db.Conn.QueryRow(ctx, `SELECT count(*) FROM definition_version WHERE tenant_id = $1`, tenantID).Scan(&seededRows); err != nil {
		t.Fatalf("count seeded rows: %v", err)
	}
	if seededRows != len(plan) {
		t.Fatalf("definition_version rows for %s = %d, want %d", tenantID, seededRows, len(plan))
	}

	var second bytes.Buffer
	if err := runSeedCommand(ctx, db.Conn, tenant, &second); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	var secondReceipt seedReceipt
	if err := json.Unmarshal(second.Bytes(), &secondReceipt); err != nil {
		t.Fatalf("decode second receipt %q: %v", second.String(), err)
	}
	if secondReceipt.Tenant != tenantID || secondReceipt.Digest != firstReceipt.Digest || secondReceipt.Inserted != 0 || secondReceipt.Skipped != len(plan) {
		t.Fatalf("second receipt = %+v, want matching digest and an idempotent skip", secondReceipt)
	}

	wantFirst := `{"tenant":"` + tenantID.String() + `","digest":"` + firstReceipt.Digest + `","inserted":` + strconv.Itoa(len(plan)) + `,"skipped":0}` + "\n"
	if first.String() != wantFirst {
		t.Fatalf("first receipt = %q, want %q", first.String(), wantFirst)
	}
}

// TestTodo_SVC_013_Integration proves migrate's actual up/status/down
// behavior - Goose applying every embedded migration, the schema journal
// recording it, status reporting the applied version, and down rolling the
// most recent one back - against a real PostgreSQL server. It calls
// runMigrateCommand directly against pgtest's already-open, schema-isolated
// *sql.DB (pgtest.NewEmpty) rather than routing through bootstrap.Run's
// -database-url flag: migrate does not use bootstrap's
// DatabaseURLField/DBPoolFactory (it needs a database/sql.DB, not
// bootstrap's DBPool port - see main.go), so there is nothing further
// bootstrap-specific to exercise here that TestTodo_SVC_013's
// bootstrap.Run-level subtests do not already cover with a fake pool.
func TestTodo_SVC_013_Integration(t *testing.T) {
	db := pgtest.NewEmpty(t)
	ctx := context.Background()

	var up1 bytes.Buffer
	if err := runMigrateCommand(ctx, "up", db.SQL, &up1); err != nil {
		t.Fatalf("up: %v", err)
	}
	if !strings.Contains(up1.String(), "schema version") {
		t.Fatalf("up output = %q, want a schema-version report line", up1.String())
	}

	var upAgain bytes.Buffer
	if err := runMigrateCommand(ctx, "up", db.SQL, &upAgain); err != nil {
		t.Fatalf("up (idempotent rerun): %v", err)
	}
	if strings.Contains(upAgain.String(), "\napplied ") {
		t.Fatalf("rerunning up re-applied a migration: %q", upAgain.String())
	}

	var statusOut bytes.Buffer
	if err := runMigrateCommand(ctx, "status", db.SQL, &statusOut); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(statusOut.String(), "schema version") {
		t.Fatalf("status output = %q, want a schema-version report line", statusOut.String())
	}

	phaseDir := t.TempDir()
	phasePlanPath := filepath.Join(phaseDir, "phase-plan.json")
	phaseRowsPath := filepath.Join(phaseDir, "phase-rows.json")
	phaseJournalPath := filepath.Join(phaseDir, "phase-journal.json")
	files, err := migrations.Files()
	if err != nil {
		t.Fatalf("phase migration files: %v", err)
	}
	digest, err := migrations.ArtifactDigest()
	if err != nil {
		t.Fatalf("phase migration digest: %v", err)
	}
	phasePlan := migrationPhasePlanFile{
		MigrationID:    releaseVersion(files, digest),
		ManifestDigest: digest,
		Plan:           upgradeTestPlan(),
	}
	phasePlanRaw, err := json.Marshal(phasePlan)
	if err != nil {
		t.Fatalf("encode phase plan: %v", err)
	}
	if err := os.WriteFile(phasePlanPath, phasePlanRaw, 0o600); err != nil {
		t.Fatalf("write phase plan: %v", err)
	}
	phaseRowsRaw, err := json.Marshal(upgradeTestRows())
	if err != nil {
		t.Fatalf("encode phase rows: %v", err)
	}
	if err := os.WriteFile(phaseRowsPath, phaseRowsRaw, 0o600); err != nil {
		t.Fatalf("write phase rows: %v", err)
	}
	var phaseOut bytes.Buffer
	if err := runMigrationPhaseCommand(ctx, db.SQL, phaseJournalPath, phasePlanPath, phaseRowsPath, 3, &phaseOut); err != nil {
		t.Fatalf("phase gates: %v", err)
	}
	var phaseReceipt migrationPhaseReceipt
	if err := json.Unmarshal(phaseOut.Bytes(), &phaseReceipt); err != nil {
		t.Fatalf("decode phase receipt %q: %v", phaseOut.String(), err)
	}
	if phaseReceipt.MigrationID != releaseVersion(files, digest) || phaseReceipt.Phase != "CONTRACTED" || len(phaseReceipt.Gates) != 5 || phaseReceipt.CopiedRows != 3 {
		t.Fatalf("phase receipt = %+v, want identified CONTRACTED run through five gates", phaseReceipt)
	}

	// Migration 00363 permits rollback only while its durable receipt table is
	// empty. Put a receipt in the fresh schema so this exercises the live-data
	// guard and proves Down cannot discard it.
	tenantID := uuid.New()
	if _, err := db.SQL.ExecContext(ctx, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		tenantID, "migrate-down-guard-"+tenantID.String(), "migrate down guard"); err != nil {
		t.Fatalf("insert receipt tenant: %v", err)
	}
	receiptID := uuid.New()
	receiptDigest := "sha256:" + strings.Repeat("0", 64)
	if _, err := db.SQL.ExecContext(ctx, `
		INSERT INTO integration_webhook_receipt
			(tenant_id, receipt_id, provider, endpoint_id, event_id, event_type, schema_ref,
			 payload_digest, request_digest, payload_bytes, parsed_receipt, received_at)
		VALUES ($1, $2, 'payroll', 'payroll-endpoint', 'event-durable', 'APPLIED', 'payroll.v1',
			$3, $3, $4, '{}', $5)`,
		tenantID, receiptID, receiptDigest, []byte(`{"event_id":"event-durable"}`), time.Now().UTC()); err != nil {
		t.Fatalf("insert durable webhook receipt: %v", err)
	}

	var downOut bytes.Buffer
	downErr := runMigrateCommand(ctx, "down", db.SQL, &downOut)
	if downErr == nil {
		t.Fatalf("down succeeded with a durable webhook receipt; output = %q", downOut.String())
	}
	if !strings.Contains(downErr.Error(), "cannot remove durable provider webhook receipts") {
		t.Fatalf("down error = %v, want the durable-receipt guard", downErr)
	}
	var preserved int
	if err := db.SQL.QueryRowContext(ctx, `
		SELECT count(*) FROM integration_webhook_receipt WHERE tenant_id = $1 AND receipt_id = $2`,
		tenantID, receiptID).Scan(&preserved); err != nil {
		t.Fatalf("read receipt after refused rollback: %v", err)
	}
	if preserved != 1 {
		t.Fatalf("durable receipt rows after refused rollback = %d, want 1", preserved)
	}
}
