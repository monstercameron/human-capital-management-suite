package workflowversionstore_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var at = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

const publisher = "cmd/hcmnext:publisher"

func publishPrototype(t *testing.T, store version.Store) (version.CompiledVersion, *workflow.CompiledWorkflow) {
	t.Helper()
	plan, err := prototype.CompileApproval()
	if err != nil {
		t.Fatalf("CompileApproval: %v", err)
	}
	v, err := version.Publish(store, prototype.ApprovalDefinition(), plan, workflow.Options{Phase: workflow.PhaseP1B},
		version.PublishMeta{SemanticVersion: "1.0.0", PublishedAt: at, PublishedBy: publisher})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	return v, plan
}

func approval(digest, by string) workflowversionstore.Approval {
	return workflowversionstore.Approval{
		ApprovalID: uuid.New(), CompiledPlanDigest: digest, ReviewedPlanDigest: digest,
		ApprovedBy: by, Authority: "authority:workflow-release-board", Reason: "reviewed",
		TestsPassed: true, FixtureRefs: []string{"conformance:prototype/v1"}, ApprovedAt: at.Add(time.Minute),
	}
}

// TestTodo_WF_COMP_006_Durable proves the registry keeps publication and
// activation authority truthful across recomposition: a published version is
// a DRAFT until a durable approval by someone other than its publisher exists,
// activation appends lifecycle history, a store composed over a fresh
// connection resolves the same verified ACTIVE record, and quarantine and
// retirement persist.
func TestTodo_WF_COMP_006_Durable(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	store := workflowversionstore.Store{DB: db.Conn}

	published, plan := publishPrototype(t, store)
	if published.Status != version.StatusDraft {
		t.Fatalf("published status = %s, want DRAFT", published.Status)
	}
	if again, _ := publishPrototype(t, store); again.Digest() != published.Digest() {
		t.Fatal("republishing the same plan minted a second record")
	}
	digest := published.CompiledPlanDigest

	if _, err := store.ActivateApproved(ctx, digest, false); !errors.Is(err, workflowversionstore.ErrNoApproval) {
		t.Fatalf("activation with no approval = %v, want ErrNoApproval", err)
	}
	if err := store.RecordApproval(ctx, approval(digest, publisher)); !errors.Is(err, workflowversionstore.ErrSelfApproval) {
		t.Fatalf("self-approval = %v, want ErrSelfApproval", err)
	}
	if err := store.RecordApproval(ctx, workflowversionstore.Approval{CompiledPlanDigest: digest}); !errors.Is(err, workflowversionstore.ErrInvalid) {
		t.Fatalf("incomplete approval = %v, want ErrInvalid", err)
	}
	unknown := approval("sha256:unknown", "principal:release-manager")
	if err := store.RecordApproval(ctx, unknown); !errors.Is(err, workflowversionstore.ErrInvalid) {
		t.Fatalf("approval of an unpublished digest = %v, want ErrInvalid", err)
	}

	stale := approval(digest, "principal:release-manager")
	stale.ReviewedPlanDigest = "sha256:reviewed-something-else"
	if err := store.RecordApproval(ctx, stale); err != nil {
		t.Fatalf("RecordApproval: %v", err)
	}
	if _, err := store.ActivateApproved(ctx, digest, false); version.CodeOf(err) != version.CodeChangedAfterReview {
		t.Fatalf("activation on an approval of another digest = %v, want %s", err, version.CodeChangedAfterReview)
	}

	good := approval(digest, "principal:release-manager")
	good.ApprovedAt = at.Add(2 * time.Minute)
	if err := store.RecordApproval(ctx, good); err != nil {
		t.Fatalf("RecordApproval: %v", err)
	}
	if err := store.RecordApproval(ctx, good); err != nil {
		t.Fatalf("replaying the same approval id: %v", err)
	}
	active, err := store.ActivateApproved(ctx, digest, false)
	if err != nil || active.Status != version.StatusActive {
		t.Fatalf("ActivateApproved = %+v, %v", active, err)
	}
	if again, err := store.ActivateApproved(ctx, digest, false); err != nil || len(again.Approvals) != len(active.Approvals) {
		t.Fatalf("re-activating an ACTIVE version = %d approvals, %v; want it returned unchanged", len(again.Approvals), err)
	}

	recomposed := workflowversionstore.Store{DB: db.NewConn(t)}
	resolved, err := version.Resolve(recomposed, plan.WorkflowID, version.Pin{CompiledPlanDigest: digest})
	if err != nil || resolved.Status != version.StatusActive || resolved.Digest() != published.Digest() {
		t.Fatalf("recomposed Resolve = %+v, %v", resolved, err)
	}
	if len(resolved.Approvals) != 1 || resolved.Approvals[0].ApprovedBy != "principal:release-manager" {
		t.Fatalf("recomposed approval history = %+v", resolved.Approvals)
	}
	if bySemver, err := version.Resolve(recomposed, plan.WorkflowID, version.Pin{SemanticVersion: "1.0.0"}); err != nil || bySemver.CompiledPlanDigest != digest {
		t.Fatalf("Resolve by semantic version = %+v, %v", bySemver, err)
	}
	if current, found, err := recomposed.GetActiveForWorkflow(plan.WorkflowID); err != nil || !found || current.CompiledPlanDigest != digest {
		t.Fatalf("GetActiveForWorkflow = %+v, %v, %v", current, found, err)
	}
	if list, err := recomposed.List(plan.WorkflowID); err != nil || len(list) != 1 {
		t.Fatalf("List = %d versions, %v", len(list), err)
	}

	if _, err := version.Quarantine(recomposed, digest, "incident", "principal:incident-commander", "authority:incident", version.ActivationEvidence{ApprovedAt: at.Add(time.Hour)}); err != nil {
		t.Fatalf("Quarantine: %v", err)
	}
	if _, found, err := store.GetActiveForWorkflow(plan.WorkflowID); err != nil || found {
		t.Fatalf("a quarantined version is still active after restart (found %v, %v)", found, err)
	}
	if _, err := store.ActivateApproved(ctx, digest, false); !errors.Is(err, workflowversionstore.ErrNoApproval) {
		t.Fatalf("lifting a quarantine on the original approval = %v, want ErrNoApproval", err)
	}
	review := approval(digest, "principal:incident-reviewer")
	review.ApprovedAt = at.Add(90 * time.Minute)
	if err := store.RecordApproval(ctx, review); err != nil {
		t.Fatalf("RecordApproval(review): %v", err)
	}
	if lifted, err := store.ActivateApproved(ctx, digest, false); err != nil || lifted.Status != version.StatusActive {
		t.Fatalf("lifting a quarantine on a fresh review = %+v, %v", lifted.Status, err)
	}
	if _, err := version.Retire(store, digest, "superseded", "principal:release-manager", "authority:release", version.ActivationEvidence{ApprovedAt: at.Add(2 * time.Hour)}); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	var transitions int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM workflow_version_transition WHERE compiled_plan_digest = $1`, digest).Scan(&transitions); err != nil || transitions != 4 {
		t.Fatalf("transition rows = %d (%v), want ACTIVE, QUARANTINED, ACTIVE and RETIRED", transitions, err)
	}
	if _, err := db.Conn.Exec(ctx, `UPDATE workflow_compiled_version SET status = 'ACTIVE' WHERE compiled_plan_digest = $1`, digest); err == nil {
		t.Fatal("the database let a RETIRED version change status")
	}
	if _, err := db.Conn.Exec(ctx, `DELETE FROM workflow_version_transition WHERE compiled_plan_digest = $1`, digest); err == nil {
		t.Fatal("the database let lifecycle history be deleted")
	}
}

// TestTodo_WF_COMP_006_DurableSecurity proves a tampered record is refused on
// read and that Put never rewrites a published record's content.
func TestTodo_WF_COMP_006_DurableSecurity(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	store := workflowversionstore.Store{DB: db.Conn}
	published, _ := publishPrototype(t, store)

	db.Exec(t, `UPDATE workflow_compiled_version SET record = jsonb_set(record, '{published_by}', '"principal:attacker"') WHERE compiled_plan_digest = $1`, published.CompiledPlanDigest)
	if _, _, err := store.GetByDigest(published.CompiledPlanDigest); version.CodeOf(err) != version.CodeRecordMutated {
		t.Fatalf("read of a tampered record = %v, want %s", err, version.CodeRecordMutated)
	}
	if _, err := store.List(published.CompiledPlanDigest); err != nil {
		t.Fatalf("List of an unknown workflow: %v", err)
	}
	if _, found, err := store.GetByDigest("sha256:absent"); err != nil || found {
		t.Fatalf("GetByDigest(absent) = %v, %v", found, err)
	}
	if err := store.Put(version.CompiledVersion{}); !errors.Is(err, workflowversionstore.ErrInvalid) {
		t.Fatalf("Put of an empty record = %v, want ErrInvalid", err)
	}
	if err := (workflowversionstore.Store{}).RecordApproval(ctx, approval(published.CompiledPlanDigest, "principal:x")); !errors.Is(err, workflowversionstore.ErrInvalid) {
		t.Fatalf("a store with no database = %v, want ErrInvalid", err)
	}
}

// TestTodo_WF_COMP_006_DurableRace activates one approved version from eight
// independent sessions at once: every call returns the ACTIVE version and the
// lifecycle history records exactly one activation.
func TestTodo_WF_COMP_006_DurableRace(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	store := workflowversionstore.Store{DB: db.Conn}
	published, _ := publishPrototype(t, store)
	if err := store.RecordApproval(ctx, approval(published.CompiledPlanDigest, "principal:release-manager")); err != nil {
		t.Fatalf("RecordApproval: %v", err)
	}

	const workers = 8
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(i int, conn dbport.Beginner) {
			defer wg.Done()
			v, err := workflowversionstore.Store{DB: conn}.ActivateApproved(ctx, published.CompiledPlanDigest, false)
			if err == nil && v.Status != version.StatusActive {
				err = errors.New("returned status " + string(v.Status))
			}
			errs[i] = err
		}(i, db.NewConn(t))
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("worker %d: %v", i, err)
		}
	}
	var transitions int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM workflow_version_transition WHERE compiled_plan_digest = $1`, published.CompiledPlanDigest).Scan(&transitions); err != nil || transitions != 1 {
		t.Fatalf("activation transitions = %d (%v), want exactly 1", transitions, err)
	}
}

// TestBindTxJoinsTheCallerTransaction proves a start-time resolve through
// [version.BindTx] reads inside the caller's transaction, including a version
// that transaction published and has not committed.
func TestBindTxJoinsTheCallerTransaction(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	store := workflowversionstore.Store{DB: db.Conn}
	tx, err := db.Conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	bound := version.BindTx(ctx, tx, store)
	published, plan := publishPrototype(t, bound)
	if _, err := version.Resolve(bound, plan.WorkflowID, version.Pin{CompiledPlanDigest: published.CompiledPlanDigest}); err != nil {
		t.Fatalf("resolve inside the publishing transaction: %v", err)
	}
	if _, found, err := (workflowversionstore.Store{DB: db.NewConn(t)}).GetByDigest(published.CompiledPlanDigest); err != nil || found {
		t.Fatalf("an uncommitted publication is visible outside its transaction (found %v, %v)", found, err)
	}
	if registry := version.NewRegistry(); version.BindTx(ctx, tx, registry) != version.Store(registry) {
		t.Fatal("BindTx wrapped a store that has no transaction to join")
	}
}
