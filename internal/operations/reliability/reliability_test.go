package reliability_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/reliability"
)

// root walks up from this test file's own directory until it finds the
// module's go.mod, which is the repository root that DefaultManifestPath is
// relative to. It deliberately does NOT match on a directory *name*: the
// earlier version looked for an ancestor literally named "hcm-next", which
// only exists on a checkout cloned into that folder. On CI the workspace is
// named after the repository ("human-capital-management-suite"), so the loop
// walked past the filesystem root and spun forever on filepath.Dir("/") ==
// "/", hanging the package until the 10-minute test timeout killed it.
func root(t *testing.T) string {
	t.Helper()
	_, f, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed; cannot locate this test file")
	}
	d := filepath.Dir(f)
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			t.Fatalf("could not locate repo root (go.mod) starting from %s", filepath.Dir(f))
		}
		d = parent
	}
}
func TestTodo_OPS_001(t *testing.T) {
	m, e := reliability.Load(filepath.Join(root(t), reliability.DefaultManifestPath))
	if e != nil {
		t.Fatal(e)
	}
	if r := reliability.Validate(m, time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)); !r.Ready() {
		t.Fatalf("manifest rejected: %v", r.Diagnostics)
	}
	if len(m.SLIs) != 2 || len(m.SLOs) != 2 || len(m.Actions) != 1 {
		t.Fatalf("pilot contract incomplete: SLIs=%d SLOs=%d actions=%d", len(m.SLIs), len(m.SLOs), len(m.Actions))
	}
	for _, sli := range m.SLIs {
		if sli.Version == "" || sli.Query == "" || sli.Denominator == "" || sli.Window == "" || sli.StalenessBound == "" || sli.Owner == "" {
			t.Fatalf("SLI lacks a versioned query/denominator/window/owner: %+v", sli)
		}
	}
}
func TestTodo_OPS_001_Golden(t *testing.T) {
	m, _ := reliability.Load(filepath.Join(root(t), reliability.DefaultManifestPath))
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	windowStart := now.Add(-time.Hour)
	x := map[string]reliability.Measurement{"pilot.read.availability": {WindowStart: windowStart, WindowEnd: now, ObservedAt: now.Add(-time.Minute), Good: 1000, Valid: 1000, Total: 1000, LatencyP95Ms: 500}, "pilot.write.availability": {WindowStart: windowStart, WindowEnd: now, ObservedAt: now.Add(-time.Minute), Good: 992, Valid: 1000, Total: 1000, LatencyP95Ms: 900}}
	got := reliability.Evaluate(*m, x, now)
	if got[0].Status != reliability.StatusHealthy || got[1].Status != reliability.StatusAtRisk {
		t.Fatalf("results=%+v", got)
	}
	if got[1].ErrorBudgetRemaining != .2 || len(got[1].Actions) != 1 || got[1].Actions[0] != "increase sampling and review capacity" {
		t.Fatalf("at-risk budget action was not applied deterministically: %+v", got[1])
	}
	if got[0].ErrorBudgetRemaining != 1 || len(got[0].Actions) != 0 {
		t.Fatalf("healthy budget should be full with no action: %+v", got[0])
	}
}
func TestTodo_OPS_001_UnknownTelemetry(t *testing.T) {
	m, _ := reliability.Load(filepath.Join(root(t), reliability.DefaultManifestPath))
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	got := reliability.Evaluate(*m, map[string]reliability.Measurement{"pilot.read.availability": {WindowStart: now.Add(-time.Hour), WindowEnd: now, ObservedAt: now.Add(-10 * time.Minute), Good: 100, Valid: 100, Total: 100}}, now)
	if got[0].Status != reliability.StatusUnknown {
		t.Fatalf("stale result=%+v", got)
	}
}

func TestTodo_OPS_001_WindowAndActionValidation(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	m := &reliability.Manifest{Version: 1, Module: "pilot", EffectiveAt: now.Format(time.RFC3339), Owner: "ops", SLIs: []reliability.SLI{{ID: "read", Version: "v1", Capability: "read", Query: "good/valid", Denominator: "valid", Window: "1h", StalenessBound: "5m", Owner: "ops", AvailabilityTarget: .99, LatencyTargetMs: 100}}, SLOs: []reliability.SLO{{ID: "read-slo", Version: "v1", SLI: "read", Target: .99, LatencyTargetMs: 100, Window: "1h", Owner: "ops", BreachAction: "freeze", AtRiskAction: "sample"}}, Actions: []reliability.BudgetAction{{ID: "half", Version: "v1", Threshold: .5, Action: "sample", Owner: "ops"}}}
	if got := reliability.Validate(m, now); !got.Ready() {
		t.Fatalf("valid manifest rejected: %+v", got.Diagnostics)
	}
	m.SLIs[0].Denominator = ""
	if got := reliability.Validate(m, now); got.Ready() {
		t.Fatal("missing denominator must be rejected")
	}
	m.SLIs[0].Denominator = "valid observations"
	m.SLIs[0].Window = "2h"
	if got := reliability.Validate(m, now); got.Ready() {
		t.Fatal("SLO window that differs from its SLI window must be rejected")
	}
	m.SLIs[0].Window = "1h"
	m.SLOs = append(m.SLOs, m.SLOs[0])
	if got := reliability.Validate(m, now); got.Ready() {
		t.Fatal("duplicate SLO ID must be rejected")
	}
}

func BenchmarkTodo_OPS_001(b *testing.B) {
	m, err := reliability.Load(filepath.Join(root(&testing.T{}), reliability.DefaultManifestPath))
	if err != nil {
		b.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	measurements := map[string]reliability.Measurement{"pilot.read.availability": {WindowStart: now.Add(-time.Hour), WindowEnd: now, ObservedAt: now.Add(-time.Minute), Good: 1000, Valid: 1000, Total: 1000}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = reliability.Evaluate(*m, measurements, now)
	}
}
