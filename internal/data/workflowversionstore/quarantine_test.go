package workflowversionstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func activePrototype(t *testing.T, db *pgtest.DB, store workflowversionstore.Store) version.CompiledVersion {
	t.Helper()
	published, _ := publishPrototype(t, store)
	if err := store.RecordApproval(context.Background(), approval(published.CompiledPlanDigest, "principal:release-manager")); err != nil {
		t.Fatalf("RecordApproval: %v", err)
	}
	active, err := store.ActivateApproved(context.Background(), published.CompiledPlanDigest, false)
	if err != nil {
		t.Fatalf("ActivateApproved: %v", err)
	}
	return active
}

func declaration(digest string, policy workflowversionstore.LivePolicy) workflowversionstore.QuarantineDeclaration {
	return workflowversionstore.QuarantineDeclaration{
		DeclarationID: uuid.New(), CompiledPlanDigest: digest, Reason: "pay band misapplied",
		EvidenceRef: "incident:INC-4821", DeclaredBy: "principal:incident-commander", ApprovedBy: "principal:head-of-people-ops",
		Authority: "authority:incident", LivePolicy: policy, RecordedAt: at.Add(time.Hour),
	}
}

// TestTodo_WF_RUN_009 proves governed quarantine against PostgreSQL: a
// declaration needs a reason, incident evidence, a distinct approver and a
// declared live-instance disposition; it moves the version out of service
// (so new starts are refused) and publishes the disposition to the driver;
// ordinary activation cannot undo it; only a distinct reviewer with
// validation evidence lifts it, and the lift is recorded.
func TestTodo_WF_RUN_009(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	store := workflowversionstore.Store{DB: db.Conn}
	active := activePrototype(t, db, store)
	digest := active.CompiledPlanDigest

	bad := declaration(digest, workflowversionstore.LivePause)
	bad.ApprovedBy = bad.DeclaredBy
	if _, err := store.Quarantine(ctx, bad); !errors.Is(err, workflowversionstore.ErrSameReviewer) {
		t.Fatalf("self-approved quarantine = %v, want ErrSameReviewer", err)
	}
	for name, mutate := range map[string]func(*workflowversionstore.QuarantineDeclaration){
		"policy":   func(d *workflowversionstore.QuarantineDeclaration) { d.LivePolicy = "IGNORE" },
		"evidence": func(d *workflowversionstore.QuarantineDeclaration) { d.EvidenceRef = " " },
		"instant":  func(d *workflowversionstore.QuarantineDeclaration) { d.RecordedAt = time.Time{} },
	} {
		d := declaration(digest, workflowversionstore.LivePause)
		mutate(&d)
		if _, err := store.Quarantine(ctx, d); !errors.Is(err, workflowversionstore.ErrInvalid) {
			t.Errorf("declaration missing %s = %v, want ErrInvalid", name, err)
		}
	}
	if _, quarantined, err := store.LiveInstancePolicy(ctx, db.Conn, digest); err != nil || quarantined {
		t.Fatalf("an active version reports quarantined=%v (%v)", quarantined, err)
	}

	d := declaration(digest, workflowversionstore.LivePause)
	quarantined, err := store.Quarantine(ctx, d)
	if err != nil || quarantined.Status != version.StatusQuarantined {
		t.Fatalf("Quarantine = %s, %v", quarantined.Status, err)
	}
	if replay, err := store.Quarantine(ctx, d); err != nil || replay.Status != version.StatusQuarantined {
		t.Fatalf("replayed declaration = %s, %v", replay.Status, err)
	}
	if _, err := store.Quarantine(ctx, declaration(digest, workflowversionstore.LiveBlock)); version.CodeOf(err) != version.CodeNotActive {
		t.Fatalf("quarantining a quarantined version = %v, want %s", err, version.CodeNotActive)
	}
	if policy, isQuarantined, err := store.LiveInstancePolicy(ctx, db.NewConn(t), digest); err != nil || !isQuarantined || policy != "PAUSE" {
		t.Fatalf("LiveInstancePolicy = %q, %v, %v; want PAUSE", policy, isQuarantined, err)
	}
	if _, found, err := store.GetActiveForWorkflow(active.WorkflowID); err != nil || found {
		t.Fatalf("a quarantined version still resolves as active (found %v, %v); new starts would run", found, err)
	}

	late := approval(digest, "principal:release-manager")
	late.ApprovedAt = at.Add(2 * time.Hour)
	if err := store.RecordApproval(ctx, late); err != nil {
		t.Fatalf("RecordApproval: %v", err)
	}
	if _, err := store.ActivateApproved(ctx, digest, false); !errors.Is(err, workflowversionstore.ErrQuarantined) {
		t.Fatalf("ordinary activation of a quarantined version = %v, want ErrQuarantined", err)
	}

	lift := workflowversionstore.QuarantineLift{
		DeclarationID: uuid.New(), CompiledPlanDigest: digest, ReviewedBy: d.DeclaredBy,
		ValidationEvidenceRef: "validation:INC-4821/replay", Reason: "fix verified", Authority: "authority:incident",
		TestsPassed: true, RecordedAt: at.Add(3 * time.Hour),
	}
	if _, err := store.LiftQuarantine(ctx, lift); !errors.Is(err, workflowversionstore.ErrSameReviewer) {
		t.Fatalf("lift by the declarer = %v, want ErrSameReviewer", err)
	}
	if _, err := store.LiftQuarantine(ctx, workflowversionstore.QuarantineLift{CompiledPlanDigest: digest}); !errors.Is(err, workflowversionstore.ErrInvalid) {
		t.Fatalf("incomplete lift = %v, want ErrInvalid", err)
	}
	lift.ReviewedBy = "principal:incident-reviewer"
	lifted, err := store.LiftQuarantine(ctx, lift)
	if err != nil || lifted.Status != version.StatusActive {
		t.Fatalf("LiftQuarantine = %s, %v", lifted.Status, err)
	}
	if replay, err := store.LiftQuarantine(ctx, lift); err != nil || replay.Status != version.StatusActive {
		t.Fatalf("replayed lift = %s, %v", replay.Status, err)
	}
	lift.DeclarationID = uuid.New()
	if _, err := store.LiftQuarantine(ctx, lift); !errors.Is(err, workflowversionstore.ErrNotQuarantined) {
		t.Fatalf("lifting a version in service = %v, want ErrNotQuarantined", err)
	}
	if _, isQuarantined, err := store.LiveInstancePolicy(ctx, db.Conn, digest); err != nil || isQuarantined {
		t.Fatalf("a lifted version still reports quarantined (%v)", err)
	}

	var declarations int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM workflow_version_quarantine WHERE compiled_plan_digest = $1`, digest).Scan(&declarations); err != nil || declarations != 2 {
		t.Fatalf("declarations = %d (%v), want the QUARANTINE and the LIFT", declarations, err)
	}
	if _, err := db.Conn.Exec(ctx, `INSERT INTO workflow_version_quarantine
		(declaration_id, compiled_plan_digest, action, live_instance_policy, reason, evidence_ref, declared_by, approved_by, authority, recorded_at)
		VALUES ($1, $2, 'QUARANTINE', 'PAUSE', 'r', 'e', 'same', 'same', 'a', now())`, uuid.New(), digest); err == nil {
		t.Fatal("the database accepted a quarantine approved by its declarer")
	}
	if _, err := db.Conn.Exec(ctx, `DELETE FROM workflow_version_quarantine WHERE compiled_plan_digest = $1`, digest); err == nil {
		t.Fatal("the database let a quarantine declaration be deleted")
	}
}
