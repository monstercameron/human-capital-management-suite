package selectionbind

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/commercial"
	"github.com/monstercameron/human-capital-management-suite/internal/governance/legal"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotblueprint"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotcommercial"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotjurisdiction"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotprovider"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/scopeceiling"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/threatregister"
)

// TestTodo_NEXT_002_Conformance cross-checks the signed manifests against
// the live artifacts and registries they must agree with, without going
// through Evaluate: each upstream gate is re-run directly here and its
// verdict compared with the report, the ceiling's slots and the other
// artifacts' own cross-references are compared with the bindings, and the
// release/endpoint/gate views are compared with internal/commercial's
// registry.
func TestTodo_NEXT_002_Conformance(t *testing.T) {
	m := mustLoadLiveManifest(t)
	tpl := mustLoadLiveTemplate(t)
	path := func(todo string) string {
		for _, b := range m.SelectionBindings {
			if b.TodoID == todo {
				return filepath.Join(repoRoot, filepath.FromSlash(b.Path))
			}
		}
		t.Fatalf("manifest does not bind %s", todo)
		return ""
	}

	t.Run("bindings match the live artifacts and each other's cross-references", func(t *testing.T) {
		if v := VerifyBindings(repoRoot, m.SelectionBindings); len(v) != 0 {
			t.Fatalf("stale: %v", v)
		}
		ceiling := mustLoad(t, path("PHASE-001"), scopeceiling.LoadManifest)
		ceilingDigest, _ := ceiling.CanonicalDigest()
		freeze := mustLoad(t, path("COMMERCIAL-001"), pilotcommercial.LoadFreeze)
		if freeze.Intent.ScopeCeilingDigest != ceilingDigest {
			t.Errorf("COMMERCIAL-001 pins ceiling %s, bound ceiling digests to %s", freeze.Intent.ScopeCeilingDigest, ceilingDigest)
		}
		bp := mustLoad(t, path("CUSTOMER-001"), pilotblueprint.LoadBlueprint)
		for ref, todo := range map[string]string{bp.ProviderTopologyRef: "SELECT-002", bp.JurisdictionProfileRef: "SELECT-001", bp.ScopeCeilingRef: "PHASE-001"} {
			if filepath.Join(repoRoot, filepath.FromSlash(ref)) != path(todo) {
				t.Errorf("CUSTOMER-001 references %s for %s, manifest binds %s", ref, todo, path(todo))
			}
		}
		var names []string
		for _, s := range ceiling.SelectionSlots {
			names = append(names, s.Name)
			if s.Filled || s.Value != "" {
				t.Errorf("ceiling slot %s is filled; NEXT-002 resolves slots in the evaluator, never in the ceiling", s.Name)
			}
			for _, filler := range strings.Split(s.FillingTodoID, ",") {
				if !contains(slotSources[s.Name], strings.TrimSpace(filler)) {
					t.Errorf("ceiling slot %s names filler %s the evaluator does not consult", s.Name, filler)
				}
			}
		}
		if strings.Join(names, ",") != strings.Join(SlotNames, ",") {
			t.Errorf("ceiling slots %v, evaluator slots %v", names, SlotNames)
		}
		if tpl.P1AManifest.Path != gateevidence.P1AManifestPath {
			t.Errorf("P1B binds %s", tpl.P1AManifest.Path)
		}
		if live, _ := m.CanonicalDigest(); tpl.P1AManifest.Digest != live {
			t.Errorf("P1B binds P1A %s, live P1A is %s", tpl.P1AManifest.Digest, live)
		}
	})

	t.Run("every binding verdict equals the upstream gate re-run directly", func(t *testing.T) {
		r, err := Evaluate(m, liveOptions(t))
		if err != nil {
			t.Fatal(err)
		}
		provider := mustLoad(t, path("SELECT-002"), pilotprovider.LoadTopology)
		providerReal, _ := provider.SatisfiesRealProviderSelectionGate()
		profile := mustLoad(t, path("SELECT-001"), pilotjurisdiction.LoadProfile)
		status, _ := legal.ParseReviewStatus(profile.ReviewStatus)
		jurisdictionReal := len(profile.Validate()) == 0 && status.Releasable()
		bp := mustLoad(t, path("CUSTOMER-001"), pilotblueprint.LoadBlueprint)
		customerReady := pilotblueprint.Instantiate(*bp, pilotblueprint.CustomerFacts{}, *provider, *profile, evaluationDate).Overall == pilotblueprint.ReadinessReady
		register := mustLoad(t, path("THREAT-001"), threatregister.LoadRegister)
		threatBlocked, _ := register.ReleaseDecision(evaluationDate)
		freeze := mustLoad(t, path("COMMERCIAL-001"), pilotcommercial.LoadFreeze)
		mismatches, err := pilotcommercial.ConformsToLiveRegistries(*freeze, path("PHASE-001"), path("SELECT-001"), path("SELECT-002"))
		if err != nil {
			t.Fatal(err)
		}
		ceiling := mustLoad(t, path("PHASE-001"), scopeceiling.LoadManifest)

		for todo, want := range map[string]bool{
			"PHASE-001":      len(ceiling.Validate()) == 0,
			"SELECT-001":     jurisdictionReal,
			"SELECT-002":     providerReal,
			"CUSTOMER-001":   customerReady,
			"TOPOLOGY-001":   strings.HasSuffix(path("TOPOLOGY-001"), ".json"),
			"COMMERCIAL-001": len(freeze.Validate()) == 0 && len(mismatches) == 0,
			"THREAT-001":     !threatBlocked && len(register.Validate()) == 0,
		} {
			if got := bindingByTodo(r, todo).Ready; got != want {
				t.Errorf("%s: report ready=%v, upstream gate says %v (reasons %v)", todo, got, want, bindingByTodo(r, todo).Reasons)
			}
		}
		if slotByName(r, "provider").Filled != providerReal || slotByName(r, "jurisdiction").Filled != jurisdictionReal {
			t.Errorf("slot verdicts disagree with upstream gates: %+v", r.Slots)
		}
		if slotByName(r, "slo").Filled != (freeze.Evidence.SLOStatus != pilotcommercial.SLOStatusNone && providerReal) {
			t.Errorf("slo slot disagrees with COMMERCIAL-001's slo_status %s", freeze.Evidence.SLOStatus)
		}
	})

	t.Run("release, endpoint and gate views agree with internal/commercial's registry", func(t *testing.T) {
		views, err := m.Views()
		if err != nil {
			t.Fatal(err)
		}
		pkg := commercial.DefaultPilotCommercialPackage()
		if pkg.ManifestDigest != views.Release.ManifestDigest || commercial.P1AManifestDigest != views.Release.ManifestDigest {
			t.Errorf("commercial registry binds %s, release view identity is %s", pkg.ManifestDigest, views.Release.ManifestDigest)
		}
		if pkg.Release != views.Release.Release || pkg.Workflow != views.Release.Workflow {
			t.Errorf("commercial release %s/%s, manifest release %s/%s", pkg.Release, pkg.Workflow, views.Release.Release, views.Release.Workflow)
		}
		if strings.Join(pkg.Authority.ForbiddenEffects, ",") != strings.Join(views.Gate.ForbiddenEffects, ",") {
			t.Errorf("commercial forbids %v, manifest gate view forbids %v", pkg.Authority.ForbiddenEffects, views.Gate.ForbiddenEffects)
		}
		if len(pkg.Entitlements) != len(views.Endpoints) {
			t.Fatalf("commercial sells %d entitlements, manifest serves %d endpoints", len(pkg.Entitlements), len(views.Endpoints))
		}
		for i, e := range pkg.Entitlements {
			v := views.Endpoints[i]
			if e.ID != v.IntentID || strings.Join(e.Modes, ",") != strings.Join(v.Modes, ",") || e.EffectClass != v.EffectClass {
				t.Errorf("entitlement[%d] %s/%v/%s != endpoint view %s/%v/%s", i, e.ID, e.Modes, e.EffectClass, v.IntentID, v.Modes, v.EffectClass)
			}
		}
		if err := pkg.Validate(); err != nil {
			t.Errorf("commercial registry no longer validates against the manifest identity: %v", err)
		}
	})
}
