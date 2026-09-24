package application

import (
	"errors"
	"fmt"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/reliability"
)

var errPilotReliabilityUnavailable = errors.New("application: pilot reliability is unavailable")

func loadPilotReliability(now time.Time) (*reliability.Manifest, error) {
	manifest, err := reliability.LoadDefault()
	if err != nil {
		return nil, fmt.Errorf("load pilot reliability manifest: %w", err)
	}
	readiness := reliability.Validate(manifest, now)
	if !readiness.Ready() {
		return nil, fmt.Errorf("pilot reliability manifest is invalid: %v", readiness.Diagnostics)
	}
	return manifest, nil
}

// EvaluatePilotReliability applies the manifest loaded and validated by the
// production serve composition to an observation window supplied by telemetry.
func (a *App) EvaluatePilotReliability(measurements map[string]reliability.Measurement, now time.Time) ([]reliability.Result, error) {
	if a == nil || a.pilotReliability == nil {
		return nil, errPilotReliabilityUnavailable
	}
	return reliability.Evaluate(*a.pilotReliability, measurements, now), nil
}
