package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/protomap"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/migrate"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

var migopFixedInstant = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func migopConn(t *testing.T, db *pgtest.DB) *pgxadapter.Conn {
	t.Helper()
	conn := db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume %s: %v", tenancy.AppRole, err)
	}
	return conn
}

func migopInsertTenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	db.Exec(t, `
		INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-local', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`,
		id, key, "tenant "+key)
	return id
}

func migopTx(conn *pgxadapter.Conn, tenantID uuid.UUID, fn func(tx dbport.Tx) error) error {
	ctx := context.Background()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, tenantID); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// migopFixture publishes a source and a SAFE-only-delta target promotion
// version to the durable registry, starts one instance on the source,
// pauses it at the start frontier and records the safe-point checkpoint
// migrate.Migrate requires.
func migopFixture(t *testing.T, db *pgtest.DB, conn *pgxadapter.Conn, tenantID uuid.UUID) (source, target *workflow.CompiledWorkflow) {
	t.Helper()
	ctx := context.Background()
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		t.Fatalf("bootstrap capability registry: %v", err)
	}
	source, err = workflow.CompilePromotionReference(registry)
	if err != nil {
		t.Fatalf("compile source Promotion fixture: %v", err)
	}
	def := workflow.PromotionReferenceDefinition()
	def.Version++
	for i := range def.Nodes {
		if def.Nodes[i].ID == workflow.PromotionNodeEvaluateBand {
			def.Nodes[i].Governance.Purpose = "SIMULATE_MANAGEMENT_PROMOTION_REVISED"
		}
	}
	target, err = workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1A, Capabilities: registry})
	if err != nil {
		t.Fatalf("compile target Promotion fixture: %v", err)
	}

	versions := workflowversionstore.Store{DB: db.Conn}
	srcDef := workflow.PromotionReferenceDefinition()
	srcRec, err := version.Publish(versions, srcDef, source,
		workflow.Options{Phase: workflow.PhaseP1A, Capabilities: registry},
		version.PublishMeta{SemanticVersion: "1.0.0", PublishedAt: migopFixedInstant, PublishedBy: "test-publisher"})
	if err != nil {
		t.Fatalf("publish source version: %v", err)
	}
	tgtRec, err := version.Publish(versions, def, target,
		workflow.Options{Phase: workflow.PhaseP1A, Capabilities: registry},
		version.PublishMeta{SemanticVersion: "1.0.1", PublishedAt: migopFixedInstant, PublishedBy: "test-publisher"})
	if err != nil {
		t.Fatalf("publish target version: %v", err)
	}
	if _, err := version.Activate(versions, srcRec.CompiledPlanDigest, version.ActivationEvidence{
		Authorized: true, ApprovedBy: "qa-lead", Authority: "authority:release-management",
		ApprovedAt: migopFixedInstant, ReviewedPlanDigest: srcRec.CompiledPlanDigest, TestsPassed: true,
	}); err != nil {
		t.Fatalf("activate source %s: %v", srcRec.CompiledPlanDigest, err)
	}

	digester, err := protomap.NewDefaultDigester()
	if err != nil {
		t.Fatalf("protomap.NewDefaultDigester: %v", err)
	}
	interval, err := values.NewOpenInstantInterval(values.NewInstant(migopFixedInstant))
	if err != nil {
		t.Fatalf("NewOpenInstantInterval: %v", err)
	}
	tenant := values.TenantId("migop-tenant")
	spec := intent.ProposalSpec{
		IntentID: "intent:migop", Revision: 1, Tenant: tenant, OrganizationScopeID: "org:acme-test:eng",
		Subjects: []intent.SubjectReference{
			{Kind: "EMPLOYMENT", SubjectID: "employment:jane-doe-9001", AuthorityDomain: "PEOPLE"},
		},
		EffectiveTime: interval,
		ControlSnapshots: intent.ControlSnapshots{
			CapabilityRegistryDigest: "sha256:capability-registry-fixture", PolicyBundleDigest: "sha256:policy-bundle-fixture",
			LegalContextDigest: "sha256:legal-context-fixture", EntitlementDigest: "sha256:entitlement-fixture",
			ReferenceDataDigest: "sha256:reference-data-fixture", ClassificationTaxonomyDigest: "sha256:classification-taxonomy-fixture",
			DLPDecisionDigest: "sha256:dlp-decision-fixture",
		},
		CreatedBy: intent.PrincipalReference{
			PrincipalID: "principal:hr-partner-7", Kind: intent.InitiatorHuman, IdentityAssuranceRef: "assurance:1",
		},
	}
	rev, err := intent.NewProposalRevision(spec,
		intent.Definition{Ref: intent.Ref{TypeID: "hcmnext.test.workflow_migrate", Version: 1}, Family: intent.FamilyChangeRequest},
		digester, nil, func() values.Instant { return values.NewInstant(migopFixedInstant) })
	if err != nil {
		t.Fatalf("NewProposalRevision: %v", err)
	}
	memVersions := version.NewRegistry()
	memRec, err := version.Publish(memVersions, srcDef, source,
		workflow.Options{Phase: workflow.PhaseP1A, Capabilities: registry},
		version.PublishMeta{SemanticVersion: "1.0.0", PublishedAt: migopFixedInstant, PublishedBy: "test-publisher"})
	if err != nil {
		t.Fatalf("publish memory version: %v", err)
	}
	if _, err := version.Activate(memVersions, memRec.CompiledPlanDigest, version.ActivationEvidence{
		Authorized: true, ApprovedBy: "qa-lead", Authority: "authority:release-management",
		ApprovedAt: migopFixedInstant, ReviewedPlanDigest: memRec.CompiledPlanDigest, TestsPassed: true,
	}); err != nil {
		t.Fatalf("activate memory version: %v", err)
	}
	approvalFacts := runtime.MemoryApprovalFacts{ByRevisionID: map[string][]runtime.ApprovalDecisionFact{
		rev.ProposalRevisionID: {{
			DecisionID: "decision:finance-partner-1", Outcome: runtime.ApprovalOutcomeApproved,
			ProposalDigest: rev.MaterialDigest.Digest,
		}},
	}}
	startReq := runtime.StartRequest{
		TenantID: tenantID, CellID: "cell-local", StartIdempotencyKey: "migop-start",
		Resolver: migopResolver{sel: runtime.WorkflowSelection{
			WorkflowID: source.WorkflowID,
			Pin:        version.Pin{CompiledPlanDigest: source.Digest()},
			Plan:       source,
		}},
		Versions: memVersions, Proposal: runtime.ProposalBinding{Revision: rev},
		ProposalFacts: runtime.MemoryProposalFacts{}, ApprovalFacts: approvalFacts,
		ExpectedIntentID: "intent:migop", ExpectedTenant: tenant,
		BusinessSubjectRefs: []string{"employment:jane-doe-9001"},
		ExecutionMode:       workflow.ModeSimulate,
		CorrelationID:       "corr-migop",
		ResolvedContext:     map[string]string{"LegalContext": "sha256:legal-context-resolved"},
		CreatedAt:           migopFixedInstant,
	}
	var receipt runtime.StartReceipt
	if err := migopTx(conn, tenantID, func(tx dbport.Tx) error {
		var err error
		receipt, err = runtime.Start(ctx, tx, startReq)
		return err
	}); err != nil {
		t.Fatalf("runtime.Start: %v", err)
	}
	var paused runtime.PauseReceipt
	if err := migopTx(conn, tenantID, func(tx dbport.Tx) error {
		var err error
		paused, err = runtime.RequestPause(ctx, tx, runtime.PauseRequest{
			TenantID: tenantID, InstanceID: receipt.InstanceID, ExpectedInstanceVersion: receipt.InstanceVersion,
			Plan: source, Reason: "MIGRATION_REVIEW", RequestedBy: "principal:operations-duty", RequestedAt: migopFixedInstant,
		})
		return err
	}); err != nil {
		t.Fatalf("RequestPause: %v", err)
	}
	if paused.Status != runtime.InstancePaused {
		t.Fatalf("pause produced status=%s, want PAUSED", paused.Status)
	}
	const placeholder = "0000000000000000000000000000000000000000000000000000000000000000"
	if err := migopTx(conn, tenantID, func(tx dbport.Tx) error {
		inst, err := (runtime.Store{}).LoadInstance(ctx, tx, tenantID, receipt.InstanceID)
		if err != nil {
			return err
		}
		return (runtimestate.CheckpointStore{}).Take(ctx, tx, runtimestate.Checkpoint{
			TenantID: tenantID, InstanceID: inst.InstanceID, Sequence: 1,
			Kind:            runtimestate.CheckpointSafePoint,
			StateDigest:     placeholder,
			FrontierDigest:  migrate.FrontierDigest(inst.CurrentNodeIDs),
			VariableDigest:  placeholder,
			InstanceVersion: uint64(inst.InstanceVersion),
			TakenAt:         migopFixedInstant,
		})
	}); err != nil {
		t.Fatalf("take safe-point checkpoint: %v", err)
	}
	// The target version is released after the instance starts, superseding
	// the source for new starts while the live instance keeps its pin: the
	// exact shape a migration is for.
	versions = workflowversionstore.Store{DB: db.Conn}
	if _, err := version.Activate(versions, tgtRec.CompiledPlanDigest, version.ActivationEvidence{
		Authorized: true, ApprovedBy: "qa-lead", Authority: "authority:release-management",
		ApprovedAt: migopFixedInstant, ReviewedPlanDigest: tgtRec.CompiledPlanDigest, TestsPassed: true,
		SupersedeActive: true,
	}); err != nil {
		t.Fatalf("activate target %s: %v", tgtRec.CompiledPlanDigest, err)
	}
	migopLastInstance = receipt.InstanceID
	return source, target
}

var migopLastInstance uuid.UUID

type migopResolver struct {
	sel runtime.WorkflowSelection
}

func (s migopResolver) ResolveWorkflow(context.Context, runtime.StartRequest) (runtime.WorkflowSelection, error) {
	return s.sel, nil
}

func TestWorkflowVersionMigrateBadInvocations(t *testing.T) {
	registry := openFixed(failingRegistry{Registry: version.NewRegistry(), err: workflowversionstore.ErrInvalid})
	for name, args := range map[string][]string{
		"preview needs flags": {"migrate-preview", "-database-url", "postgres://test"},
		"execute needs flags": {"migrate-execute", "-database-url", "postgres://test"},
		"preview bad tenant": {"migrate-preview", "-database-url", "postgres://test",
			"-tenant", "not-a-uuid", "-instance", uuid.New().String(),
			"-source-digest", "sha256:x", "-target-digest", "sha256:y"},
		"preview bad instance": {"migrate-preview", "-database-url", "postgres://test",
			"-tenant", uuid.New().String(), "-instance", "not-a-uuid",
			"-source-digest", "sha256:x", "-target-digest", "sha256:y"},
		"execute needs approval": {"migrate-execute", "-database-url", "postgres://test",
			"-tenant", uuid.New().String(), "-instance", uuid.New().String(),
			"-source-digest", "sha256:x", "-target-digest", "sha256:y"},
	} {
		var stdout, stderr bytes.Buffer
		if got := runWorkflowVersion(args, &stdout, &stderr, versionCommandNow, registry); got != 2 {
			t.Errorf("%s: exit %d, want 2 (stderr %q)", name, got, stderr.String())
		}
	}
}

// TestWorkflowVersionMigratePreviewExecute drives the served operator path
// end to end against embedded PostgreSQL: migrate-preview classifies the
// paused instance through the served command, then migrate-execute commits
// the migration and the stored instance pins the target plan.
func TestWorkflowVersionMigratePreviewExecute(t *testing.T) {
	db := pgtest.New(t)
	conn := migopConn(t, db)
	tenantID := migopInsertTenant(t, db, "migop-served")
	source, target := migopFixture(t, db, conn, tenantID)
	instanceID := migopLastInstance

	old := openMigrationPool
	openMigrationPool = func(context.Context, string) (dbport.Beginner, func(), error) {
		return db.Conn, func() {}, nil
	}
	defer func() { openMigrationPool = old }()

	run := func(args ...string) (int, string, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := runWorkflowVersion(args, &stdout, &stderr, versionCommandNow, openFixed(workflowversionstore.Store{DB: db.Conn}))
		return code, stdout.String(), stderr.String()
	}
	base := []string{"-database-url", "postgres://test",
		"-tenant", tenantID.String(), "-instance", instanceID.String(),
		"-source-digest", source.Digest(), "-target-digest", target.Digest()}

	code, out, stderr := run(append([]string{"migrate-preview"}, base...)...)
	if code != 0 {
		t.Fatalf("migrate-preview: exit %d, stdout %q, stderr %q", code, out, stderr)
	}
	if !strings.Contains(out, "preview_digest:") || !strings.Contains(out, "outcome=SAFE") {
		t.Fatalf("migrate-preview printed %q, want a sealed SAFE classification", out)
	}

	execArgs := append([]string{"migrate-execute"}, base...)
	execArgs = append(execArgs, "-approved-by", "principal:release-manager",
		"-reason", "REVIEWED_MIGRATION_PLAN", "-migrated-by", "principal:migration-operator")
	code, out, stderr = run(execArgs...)
	if code != 0 {
		t.Fatalf("migrate-execute: exit %d, stdout %q, stderr %q", code, out, stderr)
	}
	if !strings.Contains(out, "receipt_digest:") {
		t.Fatalf("migrate-execute printed %q, want a receipt digest", out)
	}

	var hash string
	var status runtime.InstanceStatus
	if err := migopTx(conn, tenantID, func(tx dbport.Tx) error {
		inst, err := (runtime.Store{}).LoadInstance(context.Background(), tx, tenantID, instanceID)
		if err != nil {
			return err
		}
		hash, status = inst.CompiledPlanHash, inst.RuntimeStatus
		return nil
	}); err != nil {
		t.Fatalf("load migrated instance: %v", err)
	}
	if hash != target.Digest() {
		t.Fatalf("stored instance pins %s, want target %s", hash, target.Digest())
	}
	if status != runtime.InstancePaused {
		t.Fatalf("status after served migration = %s, want PAUSED", status)
	}
}
