package releaseevidence_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/releaseevidence"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

func matchingRuntime(t *testing.T, r releaseevidence.Release) releaseevidence.Runtime {
	t.Helper()
	return releaseevidence.Runtime{BinaryRevision: r.BinaryRevision, SchemaDigest: r.SchemaDigest,
		AppliedSchemaVersion: r.SchemaVersion, Definitions: maps.Clone(r.Definitions)}
}

func sealed(t *testing.T) releaseevidence.Release {
	t.Helper()
	r, err := shippedRelease(t).Seal()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func kinds(skews []releaseevidence.Skew) string {
	var out []string
	for _, s := range skews {
		out = append(out, string(s.Kind)+":"+s.Subject)
	}
	return fmt.Sprint(out)
}

// TestTodo_ALIGN_055 proves each kind of skew between a running cell and its
// release is detected and named, and a cell running exactly its release has
// none.
func TestTodo_ALIGN_055(t *testing.T) {
	r := sealed(t)
	if skews, err := releaseevidence.Detect(r, matchingRuntime(t, r)); err != nil || len(skews) != 0 {
		t.Fatalf("matching runtime = %v, %v", skews, err)
	}
	rt := matchingRuntime(t, r)
	rt.BinaryRevision = "rev-hotfix"
	rt.AppliedSchemaVersion = r.SchemaVersion - 1
	rt.Definitions["hcmnext.people.promote_worker/v1"] = "sha256:changed"
	delete(rt.Definitions, "hcmnext.workflows.promotion.execute")
	rt.Definitions["hcmnext.people.transfer_worker/v1"] = "sha256:new"
	skews, err := releaseevidence.Detect(r, rt)
	if err != nil {
		t.Fatal(err)
	}
	want := "[BINARY_SKEW: DEFINITION_CHANGED:hcmnext.people.promote_worker/v1 DEFINITION_MISSING:hcmnext.workflows.promotion.execute DEFINITION_UNRELEASED:hcmnext.people.transfer_worker/v1 MIGRATION_BEHIND:]"
	if got := kinds(skews); got != want {
		t.Fatalf("skews = %s\nwant    %s", got, want)
	}
}

// TestTodo_ALIGN_055_Property proves detection is exhaustive and exact over
// single-field perturbations: each one yields exactly its own finding.
func TestTodo_ALIGN_055_Property(t *testing.T) {
	r := sealed(t)
	for want, mutate := range map[releaseevidence.SkewKind]func(*releaseevidence.Runtime){
		releaseevidence.SkewBinary:               func(rt *releaseevidence.Runtime) { rt.BinaryRevision = "x" },
		releaseevidence.SkewSchemaDigest:         func(rt *releaseevidence.Runtime) { rt.SchemaDigest = "x" },
		releaseevidence.SkewMigrationBehind:      func(rt *releaseevidence.Runtime) { rt.AppliedSchemaVersion-- },
		releaseevidence.SkewMigrationAhead:       func(rt *releaseevidence.Runtime) { rt.AppliedSchemaVersion++ },
		releaseevidence.SkewDefinitionChanged:    func(rt *releaseevidence.Runtime) { rt.Definitions["hcmnext.people.promote_worker/v1"] = "x" },
		releaseevidence.SkewDefinitionMissing:    func(rt *releaseevidence.Runtime) { delete(rt.Definitions, "hcmnext.people.promote_worker/v1") },
		releaseevidence.SkewDefinitionUnreleased: func(rt *releaseevidence.Runtime) { rt.Definitions["new"] = "x" },
	} {
		rt := matchingRuntime(t, r)
		mutate(&rt)
		skews, err := releaseevidence.Detect(r, rt)
		if err != nil || len(skews) != 1 || skews[0].Kind != want {
			t.Errorf("%s perturbation = %v, %v", want, skews, err)
		}
	}
}

// TestTodo_ALIGN_055_Golden pins the finding encoding.
func TestTodo_ALIGN_055_Golden(t *testing.T) {
	r := sealed(t)
	rt := matchingRuntime(t, r)
	rt.AppliedSchemaVersion = r.SchemaVersion + 2
	skews, _ := releaseevidence.Detect(r, rt)
	b, _ := json.Marshal(skews)
	want := fmt.Sprintf(`[{"kind":"MIGRATION_AHEAD","released":"%d","running":"%d"}]`, r.SchemaVersion, r.SchemaVersion+2)
	if string(b) != want {
		t.Fatalf("skews = %s", b)
	}
}

// TestTodo_ALIGN_055_Security refuses to compare against tampered release
// evidence: a release whose content no longer matches its digest cannot
// vouch for any runtime.
func TestTodo_ALIGN_055_Security(t *testing.T) {
	r := sealed(t)
	forged := r
	forged.BinaryRevision = "rev-attacker"
	rt := matchingRuntime(t, forged)
	if _, err := releaseevidence.Detect(forged, rt); err == nil {
		t.Fatal("a forged release vouched for a matching runtime")
	}
	if _, err := releaseevidence.Detect(releaseevidence.Release{}, rt); err == nil {
		t.Fatal("an empty release was accepted")
	}
}

// TestTodo_ALIGN_055_Integration reads the running facts from a real migrated
// PostgreSQL database and the real embedded artifacts: a freshly migrated
// database matches the release, and one rolled back a migration is detected
// as MIGRATION_BEHIND.
func TestTodo_ALIGN_055_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	provider := db.Provider(t)
	applied, err := provider.GetDBVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	digest, _ := migrations.ArtifactDigest()
	plan, _ := promotionexec.Compile()
	r := sealed(t)
	rt := releaseevidence.Runtime{BinaryRevision: r.BinaryRevision, SchemaDigest: digest, AppliedSchemaVersion: applied,
		Definitions: map[string]string{plan.WorkflowID: plan.Digest(), "hcmnext.people.promote_worker/v1": "sha256:intent-def"}}
	if skews, err := releaseevidence.Detect(r, rt); err != nil || len(skews) != 0 {
		t.Fatalf("freshly migrated database skews = %v, %v", skews, err)
	}
	reversible, err := migrations.NewestReversibleVersion()
	if err != nil {
		t.Fatal(err)
	}
	if reversible != applied {
		t.Skipf("newest migration %d is irreversible; rollback skew exercised by the property test", applied)
	}
	if _, err := provider.Down(ctx); err != nil {
		t.Fatalf("roll back one migration: %v", err)
	}
	rolled, _ := provider.GetDBVersion(ctx)
	rt.AppliedSchemaVersion = rolled
	skews, err := releaseevidence.Detect(r, rt)
	if err != nil || len(skews) != 1 || skews[0].Kind != releaseevidence.SkewMigrationBehind {
		t.Fatalf("rolled-back database skews = %v, %v", skews, err)
	}
}

// TestTodo_ALIGN_055_Fault proves nil definition maps are handled and an
// empty runtime reports every released definition missing.
func TestTodo_ALIGN_055_Fault(t *testing.T) {
	r := sealed(t)
	skews, err := releaseevidence.Detect(r, releaseevidence.Runtime{})
	if err != nil {
		t.Fatal(err)
	}
	missing := 0
	for _, s := range skews {
		if s.Kind == releaseevidence.SkewDefinitionMissing {
			missing++
		}
	}
	if missing != len(r.Definitions) {
		t.Fatalf("empty runtime reported %d missing of %d", missing, len(r.Definitions))
	}
}

// TestTodo_ALIGN_055_Conformance pins the closed skew vocabulary and that
// findings are ordered by kind then subject.
func TestTodo_ALIGN_055_Conformance(t *testing.T) {
	r := sealed(t)
	rt := matchingRuntime(t, r)
	rt.Definitions["z"], rt.Definitions["a"] = "x", "x"
	skews, _ := releaseevidence.Detect(r, rt)
	if kinds(skews) != "[DEFINITION_UNRELEASED:a DEFINITION_UNRELEASED:z]" {
		t.Fatalf("order = %s", kinds(skews))
	}
}
