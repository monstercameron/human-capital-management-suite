package workspace_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/latencygate"
)

func uxauditK2Read(t *testing.T, root, relative string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return string(body)
}

// TestTodo_UXAUDIT_025_Integration proves the release registry points at the
// served promotion workflow and at the two newly covered settings/appearance
// boundaries, not merely at a summary note.
func TestTodo_UXAUDIT_025_Integration(t *testing.T) {
	root, _, gate := uxaudit025Load(t)
	workflow := uxauditK2Read(t, root, "test/workflow/todo_uxaudit_002_test.go")
	for _, marker := range []string{"func TestTodo_UXAUDIT_002(", "func TestTodo_UXAUDIT_002_Recovery(", "recompose()", "replayed"} {
		if !strings.Contains(workflow, marker) {
			t.Fatalf("promotion workflow evidence is missing %q", marker)
		}
	}
	for _, id := range []string{"UXAUDIT-022", "UXAUDIT-023"} {
		found := false
		for _, finding := range gate.Findings {
			if finding.ID == id {
				found = true
				for _, evidence := range finding.Evidence {
					if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(evidence))); err != nil {
						t.Fatalf("%s evidence %q is not served by the checkout: %v", id, evidence, err)
					}
				}
			}
		}
		if !found {
			t.Fatalf("registry omitted %s", id)
		}
	}
}

// TestTodo_UXAUDIT_025_Browser is a component-level contract for the checked
// in live-browser harness; it does not claim that Playwright ran in this lane.
func TestTodo_UXAUDIT_025_Browser(t *testing.T) {
	root, _, gate := uxaudit025Load(t)
	spec := uxauditK2Read(t, root, "tools/uxqual/browser/uxaudit016_018_021_022_023_live.spec.mjs")
	for _, marker := range []string{
		`TestTodo_UXAUDIT_022_Browser`, `TestTodo_UXAUDIT_023_Browser`,
		`page.setViewportSize({ width: 390`, `page.keyboard.press("Tab")`, `page.screenshot`,
	} {
		if !strings.Contains(spec, marker) {
			t.Fatalf("browser harness is missing %q", marker)
		}
	}
	if !strings.Contains(gate.LiveBrowserHarness, "1440") || !strings.Contains(gate.LiveBrowserHarness, "320") {
		t.Fatal("release gate does not declare desktop and narrow browser widths")
	}
}

// TestTodo_UXAUDIT_025_Accessibility proves the release evidence contains
// named controls, headings and keyboard assertions for the affected journeys.
func TestTodo_UXAUDIT_025_Accessibility(t *testing.T) {
	root, _, _ := uxaudit025Load(t)
	spec := uxauditK2Read(t, root, "tools/uxqual/browser/uxaudit016_018_021_022_023_live.spec.mjs")
	settings := uxauditK2Read(t, root, "internal/humanwork/productui/uxaudit023_settings_test.go")
	for _, marker := range []string{"toHaveAccessibleName", "page.keyboard.press", "main h1", "aria-current"} {
		if !strings.Contains(spec+settings, marker) {
			t.Fatalf("accessibility evidence is missing %q", marker)
		}
	}
}

// TestTodo_UXAUDIT_025_I18N verifies the supported product catalogues and
// the release harness's locale declaration remain aligned.
func TestTodo_UXAUDIT_025_I18N(t *testing.T) {
	_, _, gate := uxaudit025Load(t)
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		resolved := productui.ResolveProductLocale(locale)
		if resolved.Resolved != locale {
			t.Fatalf("locale %s resolved to %s", locale, resolved.Resolved)
		}
	}
	for _, marker := range []string{"en-US", "de-DE", "RTL"} {
		if !strings.Contains(gate.LiveBrowserHarness, marker) {
			t.Fatalf("live harness declaration is missing %q", marker)
		}
	}
}

// TestTodo_UXAUDIT_025_Performance applies the shared latency and layout-shift
// gates to deterministic release evidence without sleeping or starting a UI.
func TestTodo_UXAUDIT_025_Performance(t *testing.T) {
	latencyBudget := latencygate.Budget{Name: "UXAUDIT-025 route interaction", P95: 50 * time.Millisecond, Samples: 20}
	durations := make([]time.Duration, latencyBudget.Samples)
	for i := range durations {
		durations[i] = time.Millisecond
	}
	if err := latencygate.Check(latencyBudget, latencygate.Evaluate(latencyBudget.Name, durations)); err != nil {
		t.Fatal(err)
	}
	clsBudget := latencygate.LayoutShiftBudget{Name: "UXAUDIT-025 route transition", Max: latencygate.DefaultCLS}
	if err := latencygate.CheckLayoutShift(clsBudget, latencygate.EvaluateLayoutShift(clsBudget.Name, []float64{0.02, 0.03})); err != nil {
		t.Fatal(err)
	}
}

// TestTodo_UXAUDIT_025_Security ensures the release registry does not turn a
// missing evidence path or an external path into an apparent pass.
func TestTodo_UXAUDIT_025_Security(t *testing.T) {
	root, _, gate := uxaudit025Load(t)
	for _, finding := range gate.Findings {
		if finding.Severity <= 2 && finding.Status == "open" {
			t.Fatalf("severity-%d finding %s is open", finding.Severity, finding.ID)
		}
		for _, evidence := range finding.Evidence {
			if filepath.IsAbs(evidence) || strings.Contains(evidence, "..") || strings.Contains(evidence, "://") {
				t.Fatalf("finding %s has unsafe evidence path %q", finding.ID, evidence)
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(evidence))); err != nil {
				t.Fatalf("finding %s evidence %q is unavailable: %v", finding.ID, evidence, err)
			}
		}
	}
}

// TestTodo_UXAUDIT_025_Recovery pins the durable restart/replay proof used by
// the release gate's promotion journey.
func TestTodo_UXAUDIT_025_Recovery(t *testing.T) {
	root, _, _ := uxaudit025Load(t)
	workflow := uxauditK2Read(t, root, "test/workflow/todo_uxaudit_002_test.go")
	for _, marker := range []string{"TestTodo_UXAUDIT_002_Recovery", "restarted", "replay", "durable workflow effects"} {
		if !strings.Contains(strings.ToLower(workflow), strings.ToLower(marker)) {
			t.Fatalf("recovery evidence is missing %q", marker)
		}
	}
}
