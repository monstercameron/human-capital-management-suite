package selectionbind

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestTodo_REV_057_01_Integration evaluates the checked-in, signed P1A
// selection from live inputs and proves the verified THR-07 EDGE-07 control
// no longer blocks its signed binding.
func TestTodo_REV_057_01_SelectionBinding(t *testing.T) {
	m := mustLoadLiveManifest(t)
	if stale := VerifyBindings(repoRoot, m.SelectionBindings); len(stale) != 0 {
		t.Fatalf("live P1A bindings are stale: %v", stale)
	}
	report, err := Evaluate(m, liveOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	threat := bindingByTodo(report, "THREAT-001")
	if threat.TodoID == "" {
		t.Fatal("THREAT-001 binding missing from report")
	}
	joined := strings.Join(threat.Reasons, "\n")
	if !threat.Ready || strings.Contains(joined, "THR-07") || strings.Contains(joined, "release blocked") {
		t.Fatalf("THREAT-001 binding still has a THR-07 release block: ready=%v reasons=%s", threat.Ready, joined)
	}
}

// TestTodo_REV_057_01_Golden pins the live report with THR-07 mitigated while
// preserving unrelated selection-completeness reasons.
func TestTodo_REV_057_01_Golden(t *testing.T) {
	m := mustLoadLiveManifest(t)
	report, err := Evaluate(m, liveOptions(t))
	if err != nil {
		t.Fatal(err)
	}
	got, err := RenderJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(goldenReportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Fatalf("live selection report differs from %s", goldenReportPath)
	}
	threat := bindingByTodo(report, "THREAT-001")
	if !threat.Ready || strings.Contains(strings.Join(threat.Reasons, "\n"), "THR-07") {
		t.Fatalf("THREAT-001 binding = ready %v reasons %v, want ready without a THR-07 blocker", threat.Ready, threat.Reasons)
	}
	if report.Status != "INCOMPLETE" {
		t.Fatalf("overall selection status = %s, want INCOMPLETE because other selections remain unfilled", report.Status)
	}
}

// TestTodo_REV_057_01_Security confirms removing the exact-path THR-07
// mitigation blocks even when every other selection fixture is complete.
func TestTodo_REV_057_01_Security(t *testing.T) {
	fixes := make([]fix, 0, len(allFixes)-1)
	for _, candidate := range allFixes {
		if candidate != fixThreat {
			fixes = append(fixes, candidate)
		}
	}
	f := buildFixture(t, fixes...)
	report := evaluate(t, f)
	reasons := strings.Join(bindingByTodo(report, "THREAT-001").Reasons, "\n")
	if !strings.Contains(reasons, "THR-07") || !strings.Contains(reasons, "release blocked") {
		t.Fatalf("missing EDGE-07 mitigation did not block complete synthetic selections: %s", reasons)
	}
}
