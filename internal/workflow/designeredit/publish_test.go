// TestTodo_WF_UI_013 proves a designer draft reaches production through the
// governed release: the author submits for review, the designer fixtures
// run, a different principal approves, and activation follows the existing
// release steps.
package designeredit_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/workflowversionstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	platformexecution "github.com/monstercameron/human-capital-management-suite/internal/platform/execution"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/designeredit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/releasefixture"
	workflowversion "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

var wfui13Now = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

const wfui13DraftID = "01999f37-9f42-7000-8000-00000000ui13"

func wfui13Service(store *memoryDraftStore) designeredit.Service {
	return designeredit.Service{
		Store: store, Catalog: fixedCatalog{},
		NewID: func() (string, error) { return wfui13DraftID, nil },
		Now:   func() time.Time { return wfui13Now },
	}
}

func wfui13Seed(t *testing.T, store *memoryDraftStore, doc []byte, semver string) {
	t.Helper()
	if _, err := store.Save(context.Background(), values.TenantId("tenant-a"), designeredit.SaveRequest{
		DraftID: wfui13DraftID, WorkflowID: prototype.ApprovalWorkflowID, AuthorRef: "author-ana",
		SemanticVersion: semver, CommandLabel: "Author approval flow",
		Document: append(json.RawMessage(nil), doc...), ExpiresAt: wfui13Now.Add(30 * 24 * time.Hour), At: wfui13Now,
	}); err != nil {
		t.Fatalf("seed draft: %v", err)
	}
}

func wfui13Doc(t *testing.T) []byte {
	t.Helper()
	return wfui13NamedDoc(t, prototype.ApprovalDefinition().Name)
}

func wfui13NamedDoc(t *testing.T, name string) []byte {
	t.Helper()
	definition := prototype.ApprovalDefinition()
	definition.Name = name
	doc, err := json.Marshal(definition)
	if err != nil {
		t.Fatalf("marshal definition: %v", err)
	}
	return doc
}

func wfui13Submit(t *testing.T, service designeredit.Service, versions workflowversion.Store) designeredit.Submission {
	t.Helper()
	submission, err := service.SubmitForReview(context.Background(), values.TenantId("tenant-a"), "author-ana",
		versions, workflow.Options{Phase: workflow.PhaseP1B}, designeredit.SubmitRequest{
			DraftID: wfui13DraftID, ExpectedRevision: 1, ToolVersions: map[string]string{"go": "go1.26.3"},
		})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	return submission
}

func TestTodo_WF_UI_013(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	registry := workflowversionstore.Store{DB: db.Conn}

	t.Run("submit_review_approve_activate", func(t *testing.T) {
		store := &memoryDraftStore{}
		wfui13Seed(t, store, wfui13Doc(t), "1.0.0")
		submission := wfui13Submit(t, wfui13Service(store), registry)
		if submission.AuthorRef != "author-ana" || submission.Status != string(workflowversion.StatusDraft) {
			t.Fatalf("submission = %+v, want author-ana DRAFT", submission)
		}
		if want := designeredit.DesignerFixtureRefs(); !equalStrings(submission.FixtureRefs, want) {
			t.Fatalf("fixture refs = %v, want %v", submission.FixtureRefs, want)
		}
		v, found, err := registry.GetByDigest(submission.CompiledPlanDigest)
		if err != nil || !found || v.PublishedBy != "author-ana" || v.Status != workflowversion.StatusDraft {
			t.Fatalf("stored version = %+v found=%v err=%v, want author-ana DRAFT", v, found, err)
		}

		suite := designeredit.DesignerFixtures()
		report := releasefixture.Run(suite, v, "runner:designer-fixtures", wfui13Now)
		if !report.Passed() {
			t.Fatalf("designer fixtures = %+v, want all pass", report.Results)
		}
		if err := releasefixture.Verify(report, v); err != nil {
			t.Fatalf("verify report: %v", err)
		}

		approval, err := platformexecution.ApproveRelease(ctx, registry, suite, platformexecution.ReleaseApproval{
			CompiledPlanDigest: v.CompiledPlanDigest, ApprovedBy: "reviewer-rae",
			Authority: "authority:workflow-release-board", Reason: "designer review",
			Report: report, ApprovedAt: wfui13Now,
		})
		if err != nil {
			t.Fatalf("approve: %v", err)
		}
		if approval.ApprovedBy != "reviewer-rae" || !approval.TestsPassed {
			t.Fatalf("approval = %+v, want reviewer-rae with passing tests", approval)
		}

		still, _, err := registry.GetByDigest(v.CompiledPlanDigest)
		if err != nil || still.Status != workflowversion.StatusDraft {
			t.Fatalf("approved version status = %s, want DRAFT until activation", still.Status)
		}
		activated, err := registry.ActivateApproved(ctx, v.CompiledPlanDigest, false)
		if err != nil {
			t.Fatalf("activate: %v", err)
		}
		if activated.Status != workflowversion.StatusActive {
			t.Fatalf("activated status = %s, want ACTIVE", activated.Status)
		}
	})

	t.Run("resubmit_is_idempotent", func(t *testing.T) {
		store := &memoryDraftStore{}
		wfui13Seed(t, store, wfui13Doc(t), "1.0.0")
		service := wfui13Service(store)
		first := wfui13Submit(t, service, registry)
		second := wfui13Submit(t, service, registry)
		if first.CompiledPlanDigest != second.CompiledPlanDigest {
			t.Fatalf("resubmit digests %s vs %s, want identical", first.CompiledPlanDigest, second.CompiledPlanDigest)
		}
	})

	t.Run("stale_revision_is_refused", func(t *testing.T) {
		store := &memoryDraftStore{}
		wfui13Seed(t, store, wfui13Doc(t), "1.0.2")
		_, err := wfui13Service(store).SubmitForReview(ctx, values.TenantId("tenant-a"), "author-ana",
			registry, workflow.Options{Phase: workflow.PhaseP1B}, designeredit.SubmitRequest{
				DraftID: wfui13DraftID, ExpectedRevision: 7,
			})
		if !errors.Is(err, designeredit.ErrConflict) {
			t.Fatalf("stale submit err = %v, want ErrConflict", err)
		}
	})

	t.Run("uncompilable_draft_is_refused", func(t *testing.T) {
		broken := prototype.ApprovalDefinition()
		broken.Edges[0].To = "missing-node"
		doc, err := json.Marshal(broken)
		if err != nil {
			t.Fatalf("marshal broken: %v", err)
		}
		store := &memoryDraftStore{}
		wfui13Seed(t, store, doc, "1.0.3")
		_, err = wfui13Service(store).SubmitForReview(ctx, values.TenantId("tenant-a"), "author-ana",
			registry, workflow.Options{Phase: workflow.PhaseP1B}, designeredit.SubmitRequest{
				DraftID: wfui13DraftID, ExpectedRevision: 1,
			})
		if err == nil {
			t.Fatal("uncompilable draft submitted")
		}
		var diagnostics *workflow.Diagnostics
		if !errors.As(err, &diagnostics) {
			t.Fatalf("submit err = %v, want compiler diagnostics", err)
		}
	})

	t.Run("submit_ports_are_pluggable", func(t *testing.T) {
		store := &memoryDraftStore{}
		wfui13Seed(t, store, wfui13Doc(t), "1.0.4")
		submission := wfui13Submit(t, wfui13Service(store), workflowversion.NewRegistry())
		if submission.Status != string(workflowversion.StatusDraft) || submission.CompiledPlanDigest == "" {
			t.Fatalf("memory submission = %+v, want DRAFT with digest", submission)
		}
	})
}

func TestTodo_WF_UI_013_Security(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	registry := workflowversionstore.Store{DB: db.Conn}
	submit := func(t *testing.T, semver string) workflowversion.CompiledVersion {
		t.Helper()
		store := &memoryDraftStore{}
		wfui13Seed(t, store, wfui13NamedDoc(t, "Approval "+semver), semver)
		submission := wfui13Submit(t, wfui13Service(store), registry)
		v, found, err := registry.GetByDigest(submission.CompiledPlanDigest)
		if err != nil || !found {
			t.Fatalf("stored version: found=%v err=%v", found, err)
		}
		return v
	}
	approve := func(v workflowversion.CompiledVersion, by string, report releasefixture.Report) error {
		_, err := platformexecution.ApproveRelease(ctx, registry, designeredit.DesignerFixtures(), platformexecution.ReleaseApproval{
			CompiledPlanDigest: v.CompiledPlanDigest, ApprovedBy: by,
			Authority: "authority:workflow-release-board", Reason: "designer review",
			Report: report, ApprovedAt: wfui13Now,
		})
		return err
	}

	t.Run("stranger_cannot_submit", func(t *testing.T) {
		store := &memoryDraftStore{}
		wfui13Seed(t, store, wfui13Doc(t), "1.1.0")
		_, err := wfui13Service(store).SubmitForReview(ctx, values.TenantId("tenant-a"), "mallory",
			registry, workflow.Options{Phase: workflow.PhaseP1B}, designeredit.SubmitRequest{
				DraftID: wfui13DraftID, ExpectedRevision: 1,
			})
		if !errors.Is(err, designeredit.ErrNotFound) {
			t.Fatalf("stranger submit err = %v, want ErrNotFound", err)
		}
	})

	t.Run("author_cannot_approve_own_draft", func(t *testing.T) {
		v := submit(t, "1.1.1")
		report := releasefixture.Run(designeredit.DesignerFixtures(), v, "runner:designer-fixtures", wfui13Now)
		if err := approve(v, "author-ana", report); !errors.Is(err, workflowversionstore.ErrSelfApproval) {
			t.Fatalf("self-approve err = %v, want ErrSelfApproval", err)
		}
	})

	t.Run("tampered_report_is_refused", func(t *testing.T) {
		v := submit(t, "1.1.2")
		report := releasefixture.Run(designeredit.DesignerFixtures(), v, "runner:designer-fixtures", wfui13Now)
		report.Results[0].Passed = false
		if err := approve(v, "reviewer-rae", report); err == nil {
			t.Fatal("tampered report approved")
		}
	})

	t.Run("activation_needs_approval", func(t *testing.T) {
		v := submit(t, "1.1.3")
		if _, err := registry.ActivateApproved(ctx, v.CompiledPlanDigest, false); err == nil {
			t.Fatal("unapproved version activated")
		}
	})
}

func TestTodo_WF_UI_013_Golden(t *testing.T) {
	store := &memoryDraftStore{}
	wfui13Seed(t, store, wfui13Doc(t), "1.0.0")
	submission := wfui13Submit(t, wfui13Service(store), workflowversion.NewRegistry())
	got, err := json.Marshal(submission)
	if err != nil {
		t.Fatalf("marshal submission: %v", err)
	}
	want := `{"draft_id":"01999f37-9f42-7000-8000-00000000ui13","draft_revision":1,"author_ref":"author-ana","workflow_id":"hcmnext.workflows.prototype.promotion_approval","semantic_version":"1.0.0","definition_digest":"61368ddb8144ab86d2c12ec8f15245fedcbf04d439b4bf3dbc9981bc415511cf","compiled_plan_digest":"333ac0ba3533c6d9ffa1b67ab53bca9716cce8ca9aaa823b0a542d886ce6705e","fixture_refs":["fixture:workflow.designer/plan-sound@v1","fixture:workflow.designer/record-intact@v1","fixture:workflow.designer/compiler-pinned@v1"],"published_at":"2026-10-01T12:00:00Z","status":"DRAFT"}`
	if string(got) != want {
		t.Fatalf("submission golden changed:\n got %s\nwant %s", got, want)
	}
}
