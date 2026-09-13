package selectionbind

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
)

// TestTodo_NEXT_002_Property proves completeness is a pure function of the
// real-world facts present, over every one of the 64 subsets of the six
// facts the live repository lacks: the verdict is COMPLETE exactly when all
// six are present, reasons are non-empty exactly when it is INCOMPLETE, and
// each slot is filled exactly when the facts its own gates need are present.
// It also proves binding order is load-bearing: every rotation and adjacent
// swap of the required bindings is refused.
func TestTodo_NEXT_002_Property(t *testing.T) {
	t.Run("completeness over every subset of facts", func(t *testing.T) {
		for mask := 0; mask < 1<<len(allFixes); mask++ {
			var fixes []fix
			for i, f := range allFixes {
				if mask&(1<<i) != 0 {
					fixes = append(fixes, f)
				}
			}
			r := evaluate(t, buildFixture(t, fixes...))
			wantComplete := len(fixes) == len(allFixes)
			if (r.Status == StatusComplete) != wantComplete {
				t.Fatalf("fixes %v: Status = %s, want complete=%v; reasons:\n%s", fixes, r.Status, wantComplete, joinedReasons(r))
			}
			if (len(r.Reasons()) == 0) != wantComplete {
				t.Fatalf("fixes %v: %d reasons with Status %s", fixes, len(r.Reasons()), r.Status)
			}
			if len(r.ManifestReasons) != 0 {
				t.Fatalf("fixes %v: a freshly bound and signed manifest has manifest reasons %v", fixes, r.ManifestReasons)
			}
			wantSlot := map[string]bool{
				"provider":     has(fixes, fixProvider),
				"jurisdiction": has(fixes, fixJurisdiction),
				"topology":     has(fixes, fixProvider) && has(fixes, fixTopology),
				"slo":          has(fixes, fixProvider) && has(fixes, fixSLO),
			}
			for _, s := range r.Slots {
				if s.Filled != wantSlot[s.Slot] {
					t.Fatalf("fixes %v: slot %s filled=%v, want %v; reasons %v", fixes, s.Slot, s.Filled, wantSlot[s.Slot], s.Reasons)
				}
			}
			if got := bindingByTodo(r, "COMMERCIAL-001"); !got.Ready {
				t.Fatalf("fixes %v: a freeze kept consistent with its selections must stay ready, got %v", fixes, got.Reasons)
			}
		}
	})

	t.Run("binding order is part of the contract", func(t *testing.T) {
		m := mustLoadLiveManifest(t)
		n := len(m.SelectionBindings)
		reorderings := map[string][]gateevidence.SelectionBinding{}
		for k := 1; k < n; k++ {
			rotated := append(append([]gateevidence.SelectionBinding(nil), m.SelectionBindings[k:]...), m.SelectionBindings[:k]...)
			reorderings["rotate by "+string(rune('0'+k))] = rotated
		}
		for i := 0; i+1 < n; i++ {
			swapped := append([]gateevidence.SelectionBinding(nil), m.SelectionBindings...)
			swapped[i], swapped[i+1] = swapped[i+1], swapped[i]
			reorderings["swap "+m.SelectionBindings[i].TodoID] = swapped
		}
		for name, bindings := range reorderings {
			mm := cloneManifest(m)
			mm.SelectionBindings = bindings
			found := false
			for _, v := range mm.Validate() {
				found = found || strings.HasSuffix(v.Field, ".todo_id")
			}
			if !found {
				t.Errorf("%s: reordered bindings accepted by Validate", name)
			}
		}
	})
}
