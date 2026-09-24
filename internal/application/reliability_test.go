package application

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/reliability"
)

func TestTodo_OPS_001_Composition(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	manifest, err := loadPilotReliability(now)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.SLIs) != 2 || len(manifest.SLOs) != 2 || len(manifest.Actions) != 1 {
		t.Fatalf("pilot reliability manifest incomplete: %+v", manifest)
	}
	app := &App{pilotReliability: manifest}
	measurements := map[string]reliability.Measurement{
		"pilot.read.availability": {
			WindowStart: now.Add(-time.Hour), WindowEnd: now, ObservedAt: now.Add(-time.Minute),
			Good: 997, Valid: 1000, Total: 1000, LatencyP95Ms: 600,
		},
		"pilot.write.availability": {
			WindowStart: now.Add(-time.Hour), WindowEnd: now, ObservedAt: now.Add(-time.Minute),
			Good: 985, Valid: 1000, Total: 1000, LatencyP95Ms: 900,
		},
	}
	results, err := app.EvaluatePilotReliability(measurements, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Capability != "pilot.read" || results[1].Capability != "pilot.write" {
		t.Fatalf("pilot results=%+v, want read and write capabilities", results)
	}
	if results[0].Status != reliability.StatusAtRisk || results[0].SLO != "pilot.read.slo" || len(results[0].Actions) != 1 {
		t.Errorf("read SLO result=%+v, want AT_RISK with its error-budget action", results[0])
	}
	if results[1].Status != reliability.StatusBreached || results[1].SLO != "pilot.write.slo" || len(results[1].Actions) != 2 {
		t.Errorf("write SLO result=%+v, want BREACHED with breach and budget actions", results[1])
	}

	unknown, err := app.EvaluatePilotReliability(nil, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range unknown {
		if result.Status != reliability.StatusUnknown || len(result.Actions) != 0 {
			t.Errorf("missing telemetry must stay UNKNOWN and trigger no budget action: %+v", result)
		}
	}
}

func TestPilotReliabilityUnavailableWithoutServeComposition(t *testing.T) {
	if _, err := (*App)(nil).EvaluatePilotReliability(nil, time.Now()); err == nil {
		t.Fatal("nil application must not claim a pilot reliability result")
	}
	if _, err := (&App{}).EvaluatePilotReliability(nil, time.Now()); err == nil {
		t.Fatal("uncomposed application must not claim a pilot reliability result")
	}
}
