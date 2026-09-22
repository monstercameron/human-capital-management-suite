package migrate_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/migrate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/migrationpreview"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// previewServed seals one paused instance through the served operator path:
// the live step/stage pair is read from durable storage, never supplied by
// the caller.
func previewServed(t *testing.T, conn *pgxadapter.Conn, tenantID, instanceID uuid.UUID, source, target *workflow.CompiledWorkflow) migrate.PreviewRecord {
	t.Helper()
	var rec migrate.PreviewRecord
	// Preview issues no writes, so committing the surrounding read-only
	// transaction is harmless.
	if err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		var err error
		rec, err = migrate.PreviewPausedInstance(context.Background(), tx, tenantID, instanceID, source, target, nil)
		return err
	}); err != nil {
		t.Fatalf("PreviewPausedInstance: %v", err)
	}
	return rec
}

// TestTodo_REV_009_01 is the PRIMARY test: the served preview path seals a
// SAFE record for a paused instance, and the approved migration rewrites the
// pinned version while leaving the instance paused.
func TestTodo_REV_009_01(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "rev00901-primary")
	scenario := buildContinueScenario(t, conn, tenantID, "rev00901-primary")

	rec := previewServed(t, conn, tenantID, scenario.start.InstanceID, scenario.pf.Plan, scenario.target)
	if rec.Digest() == "" {
		t.Fatal("served preview carries no content digest")
	}
	assessment, found := rec.Assessment(scenario.start.InstanceID.String())
	if !found {
		t.Fatal("served preview never classified the paused instance")
	}
	if assessment.Outcome != migrationpreview.OutcomeSafe {
		t.Fatalf("served preview outcome = %s, want SAFE", assessment.Outcome)
	}

	req := scenario.request()
	req.Preview = rec
	req.Approval.PreviewDigest = rec.Digest()
	receipt, err := runMigrate(t, conn, tenantID, req)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if receipt.Outcome != migrationpreview.OutcomeSafe {
		t.Fatalf("receipt outcome = %s, want SAFE", receipt.Outcome)
	}
	if receipt.ToCompiledPlanDigest != scenario.target.Digest() {
		t.Fatalf("receipt digest = %s, want %s", receipt.ToCompiledPlanDigest, scenario.target.Digest())
	}
	if receipt.PreviewDigest != rec.Digest() {
		t.Fatalf("receipt preview digest = %q, want %q", receipt.PreviewDigest, rec.Digest())
	}

	migrated := loadInstanceForTest(t, conn, tenantID, scenario.start.InstanceID)
	if migrated.CompiledPlanHash != scenario.target.Digest() || migrated.WorkflowVersion != scenario.target.Version {
		t.Fatalf("stored instance pins version=%d hash=%s, want version=%d hash=%s",
			migrated.WorkflowVersion, migrated.CompiledPlanHash, scenario.target.Version, scenario.target.Digest())
	}
	if migrated.RuntimeStatus != runtime.InstancePaused {
		t.Fatalf("status after migration = %s, want PAUSED", migrated.RuntimeStatus)
	}
}

// TestTodo_REV_009_01_Integration drives a paused instance through preview
// then execute against embedded PostgreSQL and proves execution continues on
// the new plan: resume on the target followed by one successful advance.
func TestTodo_REV_009_01_Integration(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "rev00901-integration")
	scenario := buildContinueScenario(t, conn, tenantID, "rev00901-integration")

	rec := previewServed(t, conn, tenantID, scenario.start.InstanceID, scenario.pf.Plan, scenario.target)
	req := scenario.request()
	req.Preview = rec
	req.Approval.PreviewDigest = rec.Digest()
	if _, err := runMigrate(t, conn, tenantID, req); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	migrated := loadInstanceForTest(t, conn, tenantID, scenario.start.InstanceID)
	resumed, err := resumeFromPause(t, conn, tenantID, runtime.ResumeRequest{
		TenantID: tenantID, InstanceID: scenario.start.InstanceID, ExpectedInstanceVersion: migrated.InstanceVersion,
		Plan: scenario.target, ResolvedContext: promotionResolvedContext(),
		Reason: "MIGRATION_COMPLETE", ResumedBy: "principal:operations-duty", ResumedAt: fixedInstant,
	})
	if err != nil {
		t.Fatalf("ResumeFromPause on the target plan: %v", err)
	}
	if resumed.Status != runtime.InstanceRunning {
		t.Fatalf("resumed status = %s, want RUNNING", resumed.Status)
	}
	sink := runtime.NewMemorySink()
	if _, err := advanceOnce(t, conn, tenantID, advanceOutcome(
		tenantID, scenario.start.InstanceID, scenario.target, resumed.InstanceVersion, sink,
		workflow.PromotionNodeSnapshotWorker, workflow.OutcomeSucceeded, "sha256:snapshot-migrated",
	)); err != nil {
		t.Fatalf("Advance after migration: %v", err)
	}
}

// TestTodo_REV_009_01_Fault proves the served path refuses instead of
// migrating: a running instance cannot be previewed, and an approval naming
// a foreign preview digest never executes.
func TestTodo_REV_009_01_Fault(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "rev00901-fault")
	scenario := buildContinueScenario(t, conn, tenantID, "rev00901-fault")

	// Fault 1: resume the instance so it is RUNNING, then preview must refuse
	// with UNSAFE_POINT rather than classify a moving instance.
	migrated := loadInstanceForTest(t, conn, tenantID, scenario.start.InstanceID)
	resumed, err := resumeFromPause(t, conn, tenantID, runtime.ResumeRequest{
		TenantID: tenantID, InstanceID: scenario.start.InstanceID, ExpectedInstanceVersion: migrated.InstanceVersion,
		Plan: scenario.pf.Plan, ResolvedContext: promotionResolvedContext(),
		Reason: "FAULT_RESUME", ResumedBy: "principal:operations-duty", ResumedAt: fixedInstant,
	})
	if err != nil {
		t.Fatalf("ResumeFromPause: %v", err)
	}
	_ = resumed
	previewErr := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		_, err := migrate.PreviewPausedInstance(context.Background(), tx, tenantID, scenario.start.InstanceID, scenario.pf.Plan, scenario.target, nil)
		return err
	})
	if migrate.CodeOf(previewErr) != migrate.CodeUnsafePoint {
		t.Fatalf("preview of a running instance = %v, want %s", previewErr, migrate.CodeUnsafePoint)
	}

	// Fault 2: a forged approval digest never reaches the ledger. It runs
	// against a second, still-paused instance because the first one is now
	// RUNNING after the resume above.
	forged := buildContinueScenario(t, conn, tenantID, "rev00901-fault-forged")
	bad := forged.request()
	bad.Approval.PreviewDigest = "sha256:forged-preview-digest"
	if _, err := runMigrate(t, conn, tenantID, bad); migrate.CodeOf(err) != migrate.CodeUnapprovedDigest {
		t.Fatalf("migrate under a forged preview digest = %v, want %s", err, migrate.CodeUnapprovedDigest)
	}
}

// TestTodo_REV_009_01_Recovery proves a refused migration leaves no partial
// write: a separation-of-duties refusal keeps the source pin, and the same
// migration retried with a distinct approver commits.
func TestTodo_REV_009_01_Recovery(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "rev00901-recovery")
	scenario := buildContinueScenario(t, conn, tenantID, "rev00901-recovery")

	rec := previewServed(t, conn, tenantID, scenario.start.InstanceID, scenario.pf.Plan, scenario.target)
	blocked := scenario.request()
	blocked.Preview = rec
	blocked.Approval.PreviewDigest = rec.Digest()
	blocked.Approval.Approver = blocked.MigratedBy // same principal: must refuse
	if _, err := runMigrate(t, conn, tenantID, blocked); migrate.CodeOf(err) != migrate.CodeSeparationOfDuties {
		t.Fatalf("self-approved migrate = %v, want %s", err, migrate.CodeSeparationOfDuties)
	}
	still := loadInstanceForTest(t, conn, tenantID, scenario.start.InstanceID)
	if still.CompiledPlanHash != scenario.pf.Plan.Digest() {
		t.Fatalf("refused migration moved the pin to %s", still.CompiledPlanHash)
	}

	retry := scenario.request()
	retry.Preview = rec
	retry.Approval.PreviewDigest = rec.Digest()
	receipt, err := runMigrate(t, conn, tenantID, retry)
	if err != nil {
		t.Fatalf("retry Migrate: %v", err)
	}
	if receipt.ToCompiledPlanDigest != scenario.target.Digest() {
		t.Fatalf("retry receipt digest = %s, want %s", receipt.ToCompiledPlanDigest, scenario.target.Digest())
	}
}
