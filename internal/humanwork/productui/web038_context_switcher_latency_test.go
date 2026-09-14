//go:build !race && !covergate

package productui

import (
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/latencygate"
)

// Keep wall-clock quality measurements out of instrumented coverage and
// race builds; semantic context-switcher tests remain in the shared file.
func TestTodo_WEB_038_Latency(t *testing.T) {
	props := web038Fixture()
	budget := latencygate.Budget{Name: "context switcher projection", P95: 2 * time.Millisecond, Warmups: 3, Samples: interactionLatencySamples}
	result, err := latencygate.Measure(budget, func() error {
		_, renderErr := ui.RenderToString(ui.CreateElement(ContextSwitcher, props))
		return renderErr
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := latencygate.Check(budget, result); err != nil {
		t.Fatalf("%v (%s)", err, result)
	}
	t.Logf("%s", result)
}
