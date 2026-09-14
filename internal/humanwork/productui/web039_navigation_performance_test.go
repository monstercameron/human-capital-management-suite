//go:build !race && !covergate

package productui

import (
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/latencygate"
)

// TestTodo_WEB_039_InteractionP95 is the navigation interaction gate. It
// measures the bounded projection/filter render, excluding network latency;
// the browser router schedules the latter separately.
func TestTodo_WEB_039_InteractionP95(t *testing.T) {
	view := ApplyNavigationProjection(NewView(PagePeople, "tenant", "Taylor", "scope"), web039NavigationProjection())
	view.MenuQuery = "people"
	props := navigationSidebarProps(view)
	budget := latencygate.Budget{Name: "authorization-resolved navigation", P95: 5 * time.Millisecond, Warmups: 3, Samples: interactionLatencySamples}
	result, err := latencygate.Measure(budget, func() error {
		_, err := ui.RenderToString(ui.CreateElement(NavigationSidebar, props))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(result)
	if err := latencygate.Check(budget, result); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkTodo_WEB_039_NavigationInteraction(b *testing.B) {
	view := ApplyNavigationProjection(NewView(PagePeople, "tenant", "Taylor", "scope"), web039NavigationProjection())
	view.MenuQuery = "people"
	props := navigationSidebarProps(view)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ui.RenderToString(ui.CreateElement(NavigationSidebar, props)); err != nil {
			b.Fatal(err)
		}
	}
}
