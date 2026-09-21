package version_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	version "github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

func TestSemanticVersions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		value string
		valid bool
	}{
		{"0.1.0", true},
		{"1.0.0-alpha.1", true},
		{"1.0.0+build.9", true},
		{"", false},
		{"v1.0.0", false},
		{"1", false},
		{"1.0", false},
		{"01.0.0", false},
		{"1.0.0 ", false},
	} {
		t.Run(test.value, func(t *testing.T) {
			if got := version.ValidateSemanticVersion(test.value); (got == nil) != test.valid {
				t.Fatalf("ValidateSemanticVersion(%q) = %v, valid=%t", test.value, got, test.valid)
			}
		})
	}

	for _, test := range []struct {
		left, right string
		want        int
	}{
		{"1.10.0", "1.9.9", 1},
		{"2.0.0-alpha", "2.0.0", -1},
		{"1.0.0+one", "1.0.0+two", 0},
	} {
		got, err := version.CompareSemanticVersions(test.left, test.right)
		if err != nil || got != test.want {
			t.Fatalf("CompareSemanticVersions(%q, %q) = %d, %v; want %d", test.left, test.right, got, err, test.want)
		}
	}
	if got, err := version.NextPatchVersion("1.9.9-alpha.2+build"); err != nil || got != "1.9.10" {
		t.Fatalf("NextPatchVersion = %q, %v; want 1.9.10", got, err)
	}
}

func TestPublicationSemanticVersionIdentityAndOrdering(t *testing.T) {
	def := workflow.PromotionReferenceDefinition()
	plan := mustCompilePromotion(t)

	t.Run("invalid semantic version is refused", func(t *testing.T) {
		meta := validMeta()
		meta.SemanticVersion = "release-1"
		_, err := version.Publish(version.NewRegistry(), def, plan, promotionOptions(t), meta)
		requireCode(t, err, version.CodeInvalidSemanticVersion)
	})

	t.Run("published content cannot be relabeled", func(t *testing.T) {
		store := version.NewRegistry()
		if _, err := version.Publish(store, def, plan, promotionOptions(t), validMeta()); err != nil {
			t.Fatalf("publish v1: %v", err)
		}
		meta := validMeta()
		meta.SemanticVersion = "1.0.1"
		_, err := version.Publish(store, def, plan, promotionOptions(t), meta)
		requireCode(t, err, version.CodeSemanticVersionConflict)
	})

	t.Run("changed content cannot reuse a semantic version", func(t *testing.T) {
		store := version.NewRegistry()
		if _, err := version.Publish(store, def, plan, promotionOptions(t), validMeta()); err != nil {
			t.Fatalf("publish v1: %v", err)
		}
		opts := promotionOptions(t)
		opts.CompilerVersion = "hcmnext.workflow.compiler/semver-test"
		changed, err := workflow.Compile(def, opts)
		if err != nil {
			t.Fatalf("compile changed artifact: %v", err)
		}
		_, err = version.Publish(store, def, changed, opts, validMeta())
		requireCode(t, err, version.CodeSemanticVersionConflict)
	})

	t.Run("changed content must advance the published workflow family", func(t *testing.T) {
		store := version.NewRegistry()
		if _, err := version.Publish(store, def, plan, promotionOptions(t), validMeta()); err != nil {
			t.Fatalf("publish v1: %v", err)
		}
		opts := promotionOptions(t)
		opts.CompilerVersion = "hcmnext.workflow.compiler/older-publication"
		changed, err := workflow.Compile(def, opts)
		if err != nil {
			t.Fatalf("compile changed artifact: %v", err)
		}
		meta := validMeta()
		meta.SemanticVersion = "0.9.0"
		_, err = version.Publish(store, def, changed, opts, meta)
		requireCode(t, err, version.CodeVersionNotNewer)
	})

	t.Run("build metadata cannot manufacture a newer release", func(t *testing.T) {
		store := version.NewRegistry()
		meta := validMeta()
		meta.SemanticVersion = "1.0.0+first"
		if _, err := version.Publish(store, def, plan, promotionOptions(t), meta); err != nil {
			t.Fatalf("publish build one: %v", err)
		}
		opts := promotionOptions(t)
		opts.CompilerVersion = "hcmnext.workflow.compiler/build-metadata"
		changed, err := workflow.Compile(def, opts)
		if err != nil {
			t.Fatalf("compile changed artifact: %v", err)
		}
		meta.SemanticVersion = "1.0.0+second"
		_, err = version.Publish(store, def, changed, opts, meta)
		requireCode(t, err, version.CodeSemanticVersionConflict)
	})

	t.Run("active version only accepts a strictly newer replacement", func(t *testing.T) {
		store := version.NewRegistry()
		v1 := publishPromotion(t, store)
		if _, err := version.Activate(store, v1.CompiledPlanDigest, validEvidence(v1)); err != nil {
			t.Fatalf("activate v1: %v", err)
		}

		opts := promotionOptions(t)
		opts.CompilerVersion = "hcmnext.workflow.compiler/older-replacement"
		changed, err := workflow.Compile(def, opts)
		if err != nil {
			t.Fatalf("compile candidate: %v", err)
		}
		meta := validMeta()
		meta.SemanticVersion = "0.9.0"
		candidate, err := version.Publish(version.NewRegistry(), def, changed, opts, meta)
		if err != nil {
			t.Fatalf("publish candidate: %v", err)
		}
		if err := store.Put(candidate); err != nil {
			t.Fatalf("seed legacy downgrade candidate: %v", err)
		}
		evidence := validEvidence(candidate)
		evidence.SupersedeActive = true
		_, err = version.Activate(store, candidate.CompiledPlanDigest, evidence)
		requireCode(t, err, version.CodeVersionNotNewer)

		active, found, err := store.GetActiveForWorkflow(def.WorkflowID)
		if err != nil || !found || active.CompiledPlanDigest != v1.CompiledPlanDigest {
			t.Fatalf("active version changed after refused downgrade: found=%t digest=%q err=%v", found, active.CompiledPlanDigest, err)
		}
	})
}

func TestStoresAndRestorationRefuseNonSemanticWorkflowVersions(t *testing.T) {
	invalid := version.CompiledVersion{WorkflowID: "workflow.invalid", SemanticVersion: "release-1", CompiledPlanDigest: "sha256:invalid"}
	if err := version.NewRegistry().Put(invalid); err == nil {
		t.Fatal("Registry.Put accepted a non-SemVer workflow version")
	} else {
		requireCode(t, err, version.CodeInvalidSemanticVersion)
	}
	if _, err := version.Restore(invalid, "record-digest"); err == nil {
		t.Fatal("Restore accepted a non-SemVer workflow version")
	} else {
		requireCode(t, err, version.CodeInvalidSemanticVersion)
	}
}
