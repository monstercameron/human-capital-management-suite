package scopeceiling

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
)

func mustLoadP1AManifest(t *testing.T) gateevidence.P1AManifest {
	t.Helper()
	m, err := gateevidence.LoadP1AManifest(repoRoot + "/definitions/planning/gates/p1a-manifest.yaml")
	if err != nil {
		t.Fatalf("LoadP1AManifest: %v", err)
	}
	return *m
}

func mustLoadP1BTemplate(t *testing.T) gateevidence.P1BTemplate {
	t.Helper()
	tpl, err := gateevidence.LoadP1BTemplate(repoRoot + "/definitions/planning/gates/p1b-template.yaml")
	if err != nil {
		t.Fatalf("LoadP1BTemplate: %v", err)
	}
	return *tpl
}

// intentMatchesShortID reports whether a fully qualified ceiling intent id
// like "hcmnext.rewards.change_base_pay/v1" names the same intent as a
// short P1B contract id like "change_base_pay".
func intentMatchesShortID(fullID, shortID string) bool {
	return strings.Contains(fullID, "."+shortID+"/")
}

// TestTodo_PHASE_001_Conformance is PHASE-001's named CONFORMANCE test and
// the most important test in this package: it is the falsifiable subset
// check the todo's handoff describes - "every intent, capability, command,
// migration and workflow named in p1a-manifest.yaml must appear in your
// ceiling, because a selection must be a subset of what was available to
// select from." It reads the ceiling only as a check on the independently
// selection-bound P1A/P1B documents, never the reverse.
func TestTodo_PHASE_001_Conformance(t *testing.T) {
	ceiling := mustLoadCeiling(t)
	p1a := mustLoadP1AManifest(t)
	p1b := mustLoadP1BTemplate(t)
	live := liveEndpoints(t)

	t.Run("every P1A intent is INCLUDE in the ceiling", func(t *testing.T) {
		for _, want := range p1a.Intents {
			got, ok := findIntent(ceiling, want.ID)
			if !ok {
				t.Errorf("P1A manifest names intent %s, which is ABSENT from the Phase 1 scope ceiling - this is unauthorized scope that entered the selection without ever being authorized by the ceiling", want.ID)
				continue
			}
			if got.Disposition != Include {
				t.Errorf("P1A manifest names intent %s, but the ceiling disposition is %q, not INCLUDE", want.ID, got.Disposition)
			}
		}
	})

	t.Run("every P1A capability is INCLUDE in the ceiling and READ_ONLY", func(t *testing.T) {
		for _, want := range p1a.Capabilities {
			got, ok := findCapability(ceiling, want.ID)
			if !ok {
				t.Errorf("P1A manifest names capability %s, which is ABSENT from the Phase 1 scope ceiling", want.ID)
				continue
			}
			if got.Disposition != Include {
				t.Errorf("P1A manifest names capability %s, but the ceiling disposition is %q, not INCLUDE", want.ID, got.Disposition)
			}
			if got.EffectClass != ReadOnly {
				t.Errorf("P1A manifest requires capability %s to be READ_ONLY, but the ceiling names effect_class %q", want.ID, got.EffectClass)
			}
		}
	})

	t.Run("every P1B contract is INCLUDE in the ceiling", func(t *testing.T) {
		for _, want := range p1b.Contracts {
			found := false
			for _, it := range ceiling.Intents {
				if intentMatchesShortID(it.ID, want.ID) {
					found = true
					if it.Disposition != Include {
						t.Errorf("P1B template names contract %s, but ceiling intent %s has disposition %q, not INCLUDE", want.ID, it.ID, it.Disposition)
					}
				}
			}
			if !found {
				t.Errorf("P1B template names contract %s, which is ABSENT from the Phase 1 scope ceiling", want.ID)
			}
		}
	})

	t.Run("every generated live endpoint is INCLUDE in the ceiling", func(t *testing.T) {
		for id := range live {
			got, ok := findEndpoint(ceiling, id)
			if !ok {
				t.Errorf("generated endpoint-manifest.json names endpoint %s, which is ABSENT from the Phase 1 scope ceiling", id)
				continue
			}
			if got.Disposition != Include {
				t.Errorf("generated endpoint %s has ceiling disposition %q, not INCLUDE", id, got.Disposition)
			}
		}
	})

	t.Run("the P1A workflow is INCLUDE in the ceiling and bound to the promotion vertical slice", func(t *testing.T) {
		found := false
		for _, w := range ceiling.Workflows {
			if w.ID == p1a.Workflow {
				found = true
				if w.Disposition != Include {
					t.Errorf("P1A workflow %s has ceiling disposition %q, not INCLUDE", w.ID, w.Disposition)
				}
				if w.VerticalSlice != "promotion" {
					t.Errorf("P1A workflow %s has vertical_slice %q, want promotion", w.ID, w.VerticalSlice)
				}
			}
		}
		if !found {
			t.Errorf("P1A manifest names workflow %s, which is ABSENT from the Phase 1 scope ceiling", p1a.Workflow)
		}
	})
}
