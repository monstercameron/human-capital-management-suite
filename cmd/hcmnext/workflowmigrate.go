package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/application"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/migrate"
)

// openMigrationPool opens the database the migration operator commands run
// against. It is a variable so tests drive the same preview/execute path
// against embedded PostgreSQL without a network listener.
var openMigrationPool = func(ctx context.Context, url string) (dbport.Beginner, func(), error) {
	pool, err := pgxadapter.NewPool(ctx, url, nil)
	if err != nil {
		return nil, nil, err
	}
	return pool, pool.Close, nil
}

// runWorkflowMigrate dispatches migrate-preview and migrate-execute from
// runWorkflowVersion. It parses the tenant/instance UUIDs, opens the
// migration database and runs the matching operator path over real stores.
func runWorkflowMigrate(ctx context.Context, action, databaseURL, tenantFlag, instanceFlag, sourceDigest, targetDigest, approvedBy, reason, migratedBy string, stdout, stderr io.Writer, now func() time.Time) int {
	if tenantFlag == "" || instanceFlag == "" || sourceDigest == "" || targetDigest == "" {
		fmt.Fprintln(stderr, "hcmnext workflow-version "+action+": -tenant, -instance, -source-digest and -target-digest are required")
		return 2
	}
	tenantID, err := uuid.Parse(tenantFlag)
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext workflow-version %s: invalid -tenant: %v\n", action, err)
		return 2
	}
	instanceID, err := uuid.Parse(instanceFlag)
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext workflow-version %s: invalid -instance: %v\n", action, err)
		return 2
	}
	if action == "migrate-execute" && (approvedBy == "" || reason == "" || migratedBy == "") {
		fmt.Fprintln(stderr, "hcmnext workflow-version migrate-execute: -approved-by, -reason and -migrated-by are required")
		return 2
	}
	db, closeDB, err := openMigrationPool(ctx, databaseURL)
	if err != nil {
		fmt.Fprintf(stderr, "hcmnext workflow-version %s: %v\n", action, err)
		return 1
	}
	defer closeDB()
	switch action {
	case "migrate-preview":
		if err := runMigratePreview(ctx, db, tenantID, instanceID, sourceDigest, targetDigest, stdout); err != nil {
			fmt.Fprintf(stderr, "hcmnext workflow-version migrate-preview: %v\n", err)
			return 1
		}
		return 0
	case "migrate-execute":
		if err := runMigrateExecute(ctx, db, tenantID, instanceID, sourceDigest, targetDigest, approvedBy, reason, migratedBy, now().UTC(), stdout); err != nil {
			fmt.Fprintf(stderr, "hcmnext workflow-version migrate-execute: %v\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "hcmnext workflow-version: unknown action %q\n", action)
		return 2
	}
}

// runMigratePreview implements
// "hcmnext workflow-version migrate-preview" against db: it previews one
// paused instance's live frontier through migrate.PreviewPausedInstance and
// prints the sealed preview digest and classification.
func runMigratePreview(ctx context.Context, db dbport.Beginner, tenantID, instanceID uuid.UUID, sourceDigest, targetDigest string, stdout io.Writer) error {
	versions := workflowversionstore.Store{DB: db}
	source, target, err := application.ResolveWorkflowMigrationPlans(sourceDigest, targetDigest, versions)
	if err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	rec, previewErr := migrate.PreviewPausedInstance(ctx, tx, tenantID, instanceID, source, target, nil)
	if err := tx.Rollback(ctx); err != nil {
		return fmt.Errorf("rollback preview: %w", err)
	}
	fmt.Fprintf(stdout, "preview_digest: %s\n", rec.Digest())
	for _, a := range rec.Result.Assessments {
		fmt.Fprintf(stdout, "instance: %s outcome=%s\n", a.InstanceID, a.Outcome)
		for _, b := range a.Blockers {
			fmt.Fprintf(stdout, "  blocker: %s %s\n", b.Code, b.Detail)
		}
	}
	if previewErr != nil {
		return previewErr
	}
	return nil
}

// runMigrateExecute implements
// "hcmnext workflow-version migrate-execute" against db: it re-previews the
// paused instance to seal the exact preview the approval names, then runs
// migrate.Migrate in the same governed transaction and prints the receipt.
func runMigrateExecute(ctx context.Context, db dbport.Beginner, tenantID, instanceID uuid.UUID, sourceDigest, targetDigest, approvedBy, reason, migratedBy string, now time.Time, stdout io.Writer) error {
	if approvedBy == "" || reason == "" || migratedBy == "" {
		return fmt.Errorf("approved-by, reason and migrated-by are required")
	}
	if approvedBy == migratedBy {
		return fmt.Errorf("migration approver must be distinct from the principal executing the migration")
	}
	versions := workflowversionstore.Store{DB: db}
	source, target, err := application.ResolveWorkflowMigrationPlans(sourceDigest, targetDigest, versions)
	if err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	preview, previewErr := migrate.PreviewPausedInstance(ctx, tx, tenantID, instanceID, source, target, nil)
	if previewErr != nil {
		return previewErr
	}
	receipt, err := migrate.Migrate(ctx, tx, migrate.Request{
		TenantID: tenantID, InstanceID: instanceID,
		SourcePlan: source, TargetPlan: target,
		Preview: preview,
		Approval: migrate.Approval{
			PreviewDigest: preview.Digest(), Approver: approvedBy, Reason: reason, ApprovedAt: now.UTC(),
		},
		MigratedBy: migratedBy, MigratedAt: now.UTC(),
	})
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	fmt.Fprintf(stdout, "receipt_digest: %s\n", receipt.Digest())
	fmt.Fprintf(stdout, "migrated: %s version=%d digest=%s\n", receipt.InstanceID, receipt.ToWorkflowVersion, receipt.ToCompiledPlanDigest)
	return nil
}
