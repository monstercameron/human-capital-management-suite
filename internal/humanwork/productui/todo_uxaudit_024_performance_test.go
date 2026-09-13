//go:build !race

package productui

import (
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/latencygate"
)

// TestTodo_UXAUDIT_024_Performance is the PERFORMANCE test. GlobalSearch
// (global_search.go) calls SearchGlobalItems on every keystroke
// (OnInput -> query.Set -> re-render), so its cost is the shell's per-
// keystroke interaction latency, not a one-time page load cost. GREEN's
// "fuzzy ranked results" only stays usable if that per-keystroke cost stays
// bounded as a tenant's catalog of people, workflow instances and pages
// grows, and if the result set itself stays small enough to render (the
// perKind cap of 4 and the overall globalSearchLimit) rather than flooding
// the popover with every same-kind match.
//
// Mutation used: in globalSearchScore (global_search.go), removed the
// early-exit `if best == 0 { return 0 }` so every token scores every field
// of every item regardless of an earlier token's total miss (an easy,
// realistic regression: someone "simplifying" the loop, or removing it
// while chasing a ranking tweak). At a 200-item catalog (a plausible
// pessimistic tenant: comfortably above Harborcare's 65-person live
// dataset once workflows, pages and settings are added), this measured
// p50=3.6ms/p95=10.1ms at baseline and p50=11.0ms/p95=32.5ms mutated --
// FAIL against the 20ms budget below. Restoring the early exit made it
// PASS again.
func TestTodo_UXAUDIT_024_Performance(t *testing.T) {
	items := uxaudit024LargeCatalog(200)

	budget := latencygate.Budget{Name: "global search per-keystroke ranking", P95: 20 * time.Millisecond, Warmups: 5, Samples: interactionLatencySamples}
	result, err := latencygate.Measure(budget, func() error {
		_ = SearchGlobalItems(items, "avery patel promotion", globalSearchLimit)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(result)
	if err := latencygate.Check(budget, result); err != nil {
		t.Fatal(err)
	}

	t.Run("result set stays bounded regardless of catalog size", func(t *testing.T) {
		results := SearchGlobalItems(items, "person", globalSearchLimit)
		if len(results) > globalSearchLimit {
			t.Fatalf("SearchGlobalItems returned %d results, want at most the %d-item limit", len(results), globalSearchLimit)
		}
		perKind := map[string]int{}
		for _, result := range results {
			perKind[result.Kind]++
			if perKind[result.Kind] > 4 {
				t.Fatalf("kind %q supplied %d of %d results -- one kind must never crowd out the others", result.Kind, perKind[result.Kind], len(results))
			}
		}
	})
}

func BenchmarkTodo_UXAUDIT_024_GlobalSearch(b *testing.B) {
	items := uxaudit024LargeCatalog(4000)
	b.ReportAllocs()
	for b.Loop() {
		_ = SearchGlobalItems(items, "avery patel promotion", globalSearchLimit)
	}
}

// uxaudit024LargeCatalog builds a synthetic catalog spanning every
// GlobalSearchItem kind the shell emits, at a size well beyond any real
// tenant's people/workflow/settings count, so the performance budget above
// is measured against a deliberately pessimistic input.
func uxaudit024LargeCatalog(size int) []GlobalSearchItem {
	kinds := []string{"person", "workflow", "page", "setting", "component", "action"}
	items := make([]GlobalSearchItem, 0, size)
	for index := 0; index < size; index++ {
		kind := kinds[index%len(kinds)]
		items = append(items, GlobalSearchItem{
			ID: fmt.Sprintf("%s:synthetic-%d", kind, index), Kind: kind, KindLabel: kind,
			Label:       fmt.Sprintf("Synthetic %s record %d", kind, index),
			Description: "Team Strategy · Location Remote · Career and compensation workflow",
			Href:        fmt.Sprintf("/workspace/app/people?person=synthetic-%d", index),
			Keywords:    []string{"employee", "worker", "profile", "promotion", "career", "compensation"},
		})
	}
	// One item that genuinely matches every token in the benchmark query, so
	// the measured path still does the full ranking and per-kind-cap work a
	// real hit would require, rather than short-circuiting on an all-miss
	// catalog.
	items = append(items, GlobalSearchItem{
		ID: "person:worker-avery", Kind: "person", KindLabel: "Person", Label: "Avery Patel",
		Description: "DES2 · G6 · Product", Href: "/workspace/app/person?person=worker-avery",
		Keywords: []string{"promotion", "promote", "career", "compensation"},
	})
	return items
}
