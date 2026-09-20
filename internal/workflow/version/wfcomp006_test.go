package version_test

import (
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	version "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// validMeta is a publish meta that Publish must accept outright.
func validMeta() version.PublishMeta {
	return version.PublishMeta{
		SemanticVersion: "1.0.0",
		PublishedAt:     fixedTime(),
		PublishedBy:     "pipeline.publish/v1",
		ToolVersions:    map[string]string{"go": "1.26.3"},
		FixtureRefs:     []string{"conformance:hcmnext.workflows.promote_into_management/v1"},
	}
}

// validEvidence is activation evidence that Activate must accept for v.
func validEvidence(v version.CompiledVersion) version.ActivationEvidence {
	return version.ActivationEvidence{
		Authorized:         true,
		ApprovedBy:         "principal:release-manager",
		Authority:          "role:change-governance",
		ApprovedAt:         fixedTime().Add(time.Hour),
		ReviewedPlanDigest: v.CompiledPlanDigest,
		TestsPassed:        true,
	}
}

func publishPromotion(t *testing.T, store version.Store) version.CompiledVersion {
	t.Helper()
	def := workflow.PromotionReferenceDefinition()
	plan := mustCompilePromotion(t)
	v, err := version.Publish(store, def, plan, promotionOptions(t), validMeta())
	if err != nil {
		t.Fatalf("publish promotion reference: %v", err)
	}
	return v
}

// TestTodo_WF_COMP_006 is the WF-COMP-006 primary test: it proves every RED
// refusal and every GREEN publication/activation state the ticket names.
func TestTodo_WF_COMP_006(t *testing.T) {
	def := workflow.PromotionReferenceDefinition()

	t.Run("RED_missing_publication_time", func(t *testing.T) {
		store := version.NewRegistry()
		plan := mustCompilePromotion(t)
		meta := validMeta()
		meta.PublishedAt = time.Time{}
		_, err := version.Publish(store, def, plan, promotionOptions(t), meta)
		requireCode(t, err, version.CodeMissingPublicationTime)
	})

	t.Run("RED_missing_semantic_version", func(t *testing.T) {
		store := version.NewRegistry()
		plan := mustCompilePromotion(t)
		meta := validMeta()
		meta.SemanticVersion = ""
		_, err := version.Publish(store, def, plan, promotionOptions(t), meta)
		requireCode(t, err, version.CodeMissingSemanticVersion)
	})

	t.Run("RED_definition_does_not_compile", func(t *testing.T) {
		store := version.NewRegistry()
		plan := mustCompilePromotion(t)
		broken := def
		broken.StartNodeID = "does_not_exist"
		_, err := version.Publish(store, broken, plan, promotionOptions(t), validMeta())
		requireCode(t, err, version.CodeCompilationRejected)
	})

	t.Run("RED_plan_digest_mismatch", func(t *testing.T) {
		store := version.NewRegistry()
		// A plan compiled under a different compiler identity digests
		// differently, even from the identical definition: it is provably not
		// what recompiling def under opts produces.
		other, err := workflow.Compile(def, workflow.Options{
			Phase:           workflow.PhaseP1A,
			Capabilities:    promotionRegistry(t),
			CompilerVersion: "hcmnext.workflow.compiler/v99",
		})
		if err != nil {
			t.Fatalf("compile with alternate compiler version: %v", err)
		}
		_, err = version.Publish(store, def, other, promotionOptions(t), validMeta())
		requireCode(t, err, version.CodePlanDigestMismatch)
	})

	t.Run("GREEN_publish_mints_draft", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		if v.Status != version.StatusDraft {
			t.Fatalf("freshly published version has status %s, want DRAFT", v.Status)
		}
		if v.WorkflowID != workflow.PromotionWorkflowID {
			t.Fatalf("workflow id = %q, want %q", v.WorkflowID, workflow.PromotionWorkflowID)
		}
		if v.CompiledPlanDigest == "" || v.DefinitionDigest == "" || v.Digest() == "" {
			t.Fatal("a published version must carry all three digests")
		}
		if v.CompilerVersion != workflow.CompilerVersion {
			t.Fatalf("compiler version = %q, want the pinned compiler identity %q", v.CompilerVersion, workflow.CompilerVersion)
		}
		if len(v.CanonicalPlanBytes) == 0 {
			t.Fatal("a published version must carry the plan's canonical bytes")
		}
		if err := v.Verify(); err != nil {
			t.Fatalf("freshly minted version must verify: %v", err)
		}
		stored, found, err := store.GetByDigest(v.CompiledPlanDigest)
		if err != nil || !found {
			t.Fatalf("published version must be retrievable by its compiled-plan digest: found=%v err=%v", found, err)
		}
		if stored.Digest() != v.Digest() {
			t.Fatal("stored record and returned record must carry the same digest")
		}
	})

	t.Run("GREEN_duplicate_publish_returns_existing", func(t *testing.T) {
		store := version.NewRegistry()
		first := publishPromotion(t, store)
		plan := mustCompilePromotion(t)
		second, err := version.Publish(store, def, plan, promotionOptions(t), validMeta())
		if err != nil {
			t.Fatalf("republishing identical content: %v", err)
		}
		if second.Digest() != first.Digest() {
			t.Fatal("publishing the same compiled plan twice must return the existing version, not a new one")
		}
		all, err := store.List(def.WorkflowID)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(all) != 1 {
			t.Fatalf("duplicate publication must not create a second version, got %d", len(all))
		}
	})

	t.Run("RED_activate_unauthorized", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		ev := validEvidence(v)
		ev.Authorized = false
		_, err := version.Activate(store, v.CompiledPlanDigest, ev)
		requireCode(t, err, version.CodeUnauthorizedActivation)
	})

	t.Run("RED_activate_no_approver_named", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		ev := validEvidence(v)
		ev.ApprovedBy = ""
		_, err := version.Activate(store, v.CompiledPlanDigest, ev)
		requireCode(t, err, version.CodeUnauthorizedActivation)
	})

	t.Run("RED_activate_changed_after_review", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		ev := validEvidence(v)
		ev.ReviewedPlanDigest = "sha256:some-other-stale-review"
		_, err := version.Activate(store, v.CompiledPlanDigest, ev)
		requireCode(t, err, version.CodeChangedAfterReview)
	})

	t.Run("RED_activate_failed_test", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		ev := validEvidence(v)
		ev.TestsPassed = false
		_, err := version.Activate(store, v.CompiledPlanDigest, ev)
		requireCode(t, err, version.CodeFailedTest)
	})

	t.Run("RED_activate_unresolved_dependency", func(t *testing.T) {
		store := version.NewRegistry()
		plan := mustCompilePromotion(t)
		meta := validMeta()
		meta.DependsOn = []version.VersionRef{{WorkflowID: "hcmnext.workflows.upstream_dependency", CompiledPlanDigest: "sha256:never-published"}}
		v, err := version.Publish(store, def, plan, promotionOptions(t), meta)
		if err != nil {
			t.Fatalf("publish with a declared dependency: %v", err)
		}
		_, err = version.Activate(store, v.CompiledPlanDigest, validEvidence(v))
		requireCode(t, err, version.CodeUnresolvedDependency)
	})

	t.Run("RED_activate_unknown_record", func(t *testing.T) {
		store := version.NewRegistry()
		_, err := version.Activate(store, "sha256:never-published", version.ActivationEvidence{})
		requireCode(t, err, version.CodeUnknownRecord)
	})

	t.Run("GREEN_activate_moves_to_active", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		activated, err := version.Activate(store, v.CompiledPlanDigest, validEvidence(v))
		if err != nil {
			t.Fatalf("authorized, reviewed, tested activation must succeed: %v", err)
		}
		if activated.Status != version.StatusActive {
			t.Fatalf("status = %s, want ACTIVE", activated.Status)
		}
		if len(activated.Approvals) != 1 {
			t.Fatalf("activation must append one approval record, got %d", len(activated.Approvals))
		}
		if activated.Digest() != v.Digest() {
			t.Fatal("activation must not change the record's own content digest: only its status moved")
		}
		active, found, err := store.GetActiveForWorkflow(def.WorkflowID)
		if err != nil || !found {
			t.Fatalf("the store must report this version as active: found=%v err=%v", found, err)
		}
		if active.CompiledPlanDigest != v.CompiledPlanDigest {
			t.Fatal("GetActiveForWorkflow returned a different version than the one activated")
		}
	})

	t.Run("RED_reactivate_retired_version", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		retired, err := version.Retire(store, v.CompiledPlanDigest, "decommissioned", "principal:release-manager", "role:change-governance", version.ActivationEvidence{ApprovedAt: fixedTime()})
		if err != nil {
			t.Fatalf("retire: %v", err)
		}
		if retired.Status != version.StatusRetired {
			t.Fatalf("status = %s, want RETIRED", retired.Status)
		}
		_, err = version.Activate(store, v.CompiledPlanDigest, validEvidence(v))
		requireCode(t, err, version.CodeRetiredCannotActivate)
	})

	t.Run("GREEN_quarantine_then_reactivate", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		if _, err := version.Activate(store, v.CompiledPlanDigest, validEvidence(v)); err != nil {
			t.Fatalf("initial activation: %v", err)
		}
		quarantined, err := version.Quarantine(store, v.CompiledPlanDigest, "bad-version-response", "principal:incident-commander", "role:change-governance", version.ActivationEvidence{ApprovedAt: fixedTime().Add(2 * time.Hour)})
		if err != nil {
			t.Fatalf("quarantine: %v", err)
		}
		if quarantined.Status != version.StatusQuarantined {
			t.Fatalf("status = %s, want QUARANTINED", quarantined.Status)
		}
		if _, found, _ := store.GetActiveForWorkflow(def.WorkflowID); found {
			t.Fatal("a quarantined version must not remain the active version")
		}
		reactivated, err := version.Activate(store, v.CompiledPlanDigest, validEvidence(v))
		if err != nil {
			t.Fatalf("quarantine must be reversible through a further authorized activation: %v", err)
		}
		if reactivated.Status != version.StatusActive {
			t.Fatalf("status = %s, want ACTIVE after reactivation", reactivated.Status)
		}
		if len(reactivated.Approvals) != 3 {
			t.Fatalf("approval history must accumulate across transitions, got %d entries", len(reactivated.Approvals))
		}
	})

	t.Run("RED_competing_active_version_refused_without_supersede", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		if _, err := version.Activate(store, v.CompiledPlanDigest, validEvidence(v)); err != nil {
			t.Fatalf("initial activation: %v", err)
		}
		other, err := workflow.Compile(def, workflow.Options{
			Phase:           workflow.PhaseP1A,
			Capabilities:    promotionRegistry(t),
			CompilerVersion: "hcmnext.workflow.compiler/v2",
		})
		if err != nil {
			t.Fatalf("compile alternate plan: %v", err)
		}
		meta := validMeta()
		meta.SemanticVersion = "1.1.0"
		v2, err := version.Publish(store, def, other, workflow.Options{
			Phase:           workflow.PhaseP1A,
			Capabilities:    promotionRegistry(t),
			CompilerVersion: "hcmnext.workflow.compiler/v2",
		}, meta)
		if err != nil {
			t.Fatalf("publish alternate version: %v", err)
		}
		ev := validEvidence(v2)
		_, err = version.Activate(store, v2.CompiledPlanDigest, ev)
		requireCode(t, err, version.CodeAnotherVersionActive)

		ev.SupersedeActive = true
		activated, err := version.Activate(store, v2.CompiledPlanDigest, ev)
		if err != nil {
			t.Fatalf("activation with an explicit supersede must succeed: %v", err)
		}
		if activated.Status != version.StatusActive {
			t.Fatal("superseding activation must land the new version as ACTIVE")
		}
		superseded, found, err := store.GetByDigest(v.CompiledPlanDigest)
		if err != nil || !found {
			t.Fatalf("the superseded version must remain retrievable: found=%v err=%v", found, err)
		}
		if superseded.Status != version.StatusQuarantined {
			t.Fatalf("superseded version status = %s, want QUARANTINED", superseded.Status)
		}
		if !superseded.QuarantinedBySupersession() || activated.QuarantinedBySupersession() {
			t.Fatalf("supersession = %t/%t, want only the superseded version quarantined by supersession",
				superseded.QuarantinedBySupersession(), activated.QuarantinedBySupersession())
		}
		// A governed quarantine of its own is not a supersession.
		governed, err := version.Quarantine(store, v2.CompiledPlanDigest, "defect found", "principal:release-manager", "role:change-governance", validEvidence(v2))
		if err != nil || governed.Status != version.StatusQuarantined || governed.QuarantinedBySupersession() {
			t.Fatalf("governed quarantine = %s supersession=%t, %v; want a quarantine that is not a supersession", governed.Status, governed.QuarantinedBySupersession(), err)
		}
	})

	t.Run("REFACTOR_returned_value_never_aliases_the_store", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)
		v.CanonicalPlanBytes[0] = 0xFF
		v.FixtureRefs = append(v.FixtureRefs, "mutated-after-the-fact")

		stored, found, err := store.GetByDigest(v.CompiledPlanDigest)
		if err != nil || !found {
			t.Fatalf("stored record must still be retrievable: found=%v err=%v", found, err)
		}
		if err := stored.Verify(); err != nil {
			t.Fatalf("the stored record must still verify after the caller mutated its own copy: %v", err)
		}
		if len(stored.FixtureRefs) != 1 {
			t.Fatal("mutating a returned CompiledVersion's slice must not reach the store's own copy")
		}
	})

	t.Run("RED_resolve_empty_pin", func(t *testing.T) {
		store := version.NewRegistry()
		publishPromotion(t, store)
		_, err := version.Resolve(store, def.WorkflowID, version.Pin{})
		requireCode(t, err, version.CodeInvalidPin)
	})

	t.Run("GREEN_resolve_by_digest_and_semantic_version", func(t *testing.T) {
		store := version.NewRegistry()
		v := publishPromotion(t, store)

		byDigest, err := version.Resolve(store, def.WorkflowID, version.Pin{CompiledPlanDigest: v.CompiledPlanDigest})
		if err != nil {
			t.Fatalf("resolve by digest: %v", err)
		}
		if byDigest.Digest() != v.Digest() {
			t.Fatal("resolve-by-digest returned a different version")
		}

		bySemver, err := version.Resolve(store, def.WorkflowID, version.Pin{SemanticVersion: v.SemanticVersion})
		if err != nil {
			t.Fatalf("resolve by semantic version: %v", err)
		}
		if bySemver.Digest() != v.Digest() {
			t.Fatal("resolve-by-semantic-version returned a different version")
		}
	})

	t.Run("RED_resolve_unknown_version", func(t *testing.T) {
		store := version.NewRegistry()
		publishPromotion(t, store)
		_, err := version.Resolve(store, def.WorkflowID, version.Pin{CompiledPlanDigest: "sha256:never-published"})
		requireCode(t, err, version.CodeUnknownVersion)
	})
}

// requireCode asserts err is a *version.Error carrying code.
func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a %s refusal, got no error", code)
	}
	if !errors.Is(err, version.ErrVersion) {
		t.Fatalf("expected a workflow/version refusal, got %v", err)
	}
	if got := version.CodeOf(err); got != code {
		t.Fatalf("expected code %s, got %s (%v)", code, got, err)
	}
}
