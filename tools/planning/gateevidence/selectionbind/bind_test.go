package selectionbind

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
)

func TestLiveDigestRecomputesEveryLiveBindingAndReportsUnreadableArtifacts(t *testing.T) {
	m := mustLoadLiveManifest(t)
	for _, b := range m.SelectionBindings {
		got, err := LiveDigest(repoRoot, b)
		if err != nil {
			t.Fatalf("LiveDigest(%s): %v", b.TodoID, err)
		}
		if got != b.Digest {
			t.Errorf("%s live digest %s, bound %s", b.TodoID, got, b.Digest)
		}
	}

	empty := t.TempDir()
	for _, b := range m.SelectionBindings {
		if _, err := LiveDigest(empty, b); err == nil {
			t.Errorf("LiveDigest(%s) under an empty root succeeded", b.TodoID)
		}
	}
	violations := VerifyBindings(empty, m.SelectionBindings)
	if len(violations) != len(m.SelectionBindings) {
		t.Fatalf("VerifyBindings(empty root) = %d violations, want one per binding", len(violations))
	}
	for i, v := range violations {
		if !strings.Contains(v.Field, m.SelectionBindings[i].TodoID) || !strings.Contains(v.Issue, "cannot recompute live digest") {
			t.Errorf("violation[%d] = %v", i, v)
		}
	}
}

func TestEvaluateNamesOmittedAndUnloadableSelections(t *testing.T) {
	f := buildFixture(t, allFixes...)
	f.manifest = cloneManifest(f.manifest)
	f.manifest.SelectionBindings = f.manifest.SelectionBindings[:6] // THREAT-001 omitted
	f.manifest = signManifest(t, f.manifest, f.priv, f.pub)
	r := evaluate(t, f)
	if r.Status != StatusIncomplete || !strings.Contains(strings.Join(bindingByTodo(r, "THREAT-001").Reasons, "|"), "an omitted selection") {
		t.Fatalf("omitted THREAT-001 not named: %s\n%s", r.Status, joinedReasons(r))
	}

	g := buildFixture(t, allFixes...)
	writeFile(t, g.root, providerPath, []byte("provider: [unterminated"))
	r = evaluate(t, g)
	if !strings.Contains(strings.Join(bindingByTodo(r, "SELECT-002").Reasons, "|"), "cannot load") {
		t.Fatalf("unparseable SELECT-002 not named: %v", bindingByTodo(r, "SELECT-002").Reasons)
	}
	if !strings.Contains(strings.Join(bindingByTodo(r, "CUSTOMER-001").Reasons, "|"), "without the bound SELECT-001 and SELECT-002") {
		t.Fatalf("CUSTOMER-001 instantiated without a provider: %v", bindingByTodo(r, "CUSTOMER-001").Reasons)
	}

	h := buildFixture(t, allFixes...)
	writeFile(t, h.root, fixtureTopologyPath, []byte(`{"topology":{},"deploy":{},"extra":1}`))
	resignAndRebind(t, &h, "TOPOLOGY-001", fixtureTopologyPath, []byte(`{"topology":{},"deploy":{},"extra":1}`))
	r = evaluate(t, h)
	if !strings.Contains(strings.Join(bindingByTodo(r, "TOPOLOGY-001").Reasons, "|"), "unknown field") {
		t.Fatalf("topology decision with unknown fields accepted: %v", bindingByTodo(r, "TOPOLOGY-001").Reasons)
	}

	k := buildFixture(t, allFixes...)
	resignAndRebind(t, &k, "TOPOLOGY-001", fixtureTopologyPath, []byte(`{"topology":{},"deploy":{}}`))
	r = evaluate(t, k)
	if !strings.Contains(strings.Join(bindingByTodo(r, "TOPOLOGY-001").Reasons, "|"), "topology.Compile rejects") {
		t.Fatalf("invalid topology decision accepted: %v", bindingByTodo(r, "TOPOLOGY-001").Reasons)
	}
}

func TestEvaluateRequiresTheCeilingToCoverTheManifest(t *testing.T) {
	f := buildFixture(t, allFixes...)
	f.manifest = cloneManifest(f.manifest)
	f.manifest.Workflow = "promotion.execute/v1"
	f.manifest.Capabilities = append(f.manifest.Capabilities, gateevidence.Capability{ID: "hcmnext.people.change_manager", Version: 1, OwnerDomain: "people", EffectClass: "READ_ONLY", TestRef: "x"})
	f.manifest.Intents = append(f.manifest.Intents, gateevidence.Intent{Order: 9, ID: "hcmnext.people.change_manager/v1", Disposition: "INCLUDED"})
	f.manifest = signManifest(t, f.manifest, f.priv, f.pub)
	all := joinedReasons(evaluate(t, f))
	for _, want := range []string{
		"P1A intent hcmnext.people.change_manager/v1 is not INCLUDE at gate P1A",
		"P1A capability hcmnext.people.change_manager is not INCLUDE at gate P1A",
		"P1A workflow promotion.execute/v1 is not INCLUDE at gate P1A",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("reasons do not name %q:\n%s", want, all)
		}
	}
}
