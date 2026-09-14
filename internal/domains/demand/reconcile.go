// Forecast reconciliation: DEMAND-005 compares one forecast demand
// signal against measured actuals for the exact same interval.
//
// The comparison closes only on complete actuals over the exact
// forecast period, scope, target and unit. Stale or partial actuals
// refuse to close instead of reconciling against incomplete truth.
// The record binds forecast, actual, signed delta direction,
// magnitude, interval and both model and source versions.
package demand

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// DemandActual is one measured headcount truth for a forecast period.
// Complete is false while collection is still partial: partial truth
// never closes a comparison.
type DemandActual struct {
	ActualRef      string
	Location       string
	OrgUnit        string
	Skill          string
	Role           string
	RoleOrSkillRef string
	Period         values.EffectiveInterval
	Quantity       values.Quantity
	Unit           string
	Source         string
	SourceRef      string
	Complete       bool
}

func actualScope(a DemandActual) string { return demandScope(a.Location, a.OrgUnit) }

func actualTarget(a DemandActual) string {
	if a.RoleOrSkillRef != "" {
		return a.RoleOrSkillRef
	}
	if a.Role != "" {
		return a.Role
	}
	return a.Skill
}

// Validate implements validation.
func (a DemandActual) Validate() error {
	if strings.TrimSpace(a.ActualRef) == "" {
		return errors.New("demand actual ref is required")
	}
	if strings.TrimSpace(actualScope(a)) == "" {
		return errors.New("demand actual location or org unit is required")
	}
	if strings.TrimSpace(actualTarget(a)) == "" {
		return errors.New("demand actual role or skill ref is required")
	}
	if err := a.Period.Validate(); err != nil {
		return fmt.Errorf("demand actual period: %w", err)
	}
	if err := a.Quantity.Validate(); err != nil {
		return fmt.Errorf("demand actual quantity: %w", err)
	}
	if strings.TrimSpace(a.Unit) == "" || a.Unit != a.Quantity.Unit() {
		return errors.New("demand actual unit must equal its quantity unit")
	}
	if strings.TrimSpace(a.Source) == "" || strings.TrimSpace(a.SourceRef) == "" {
		return errors.New("demand actual source and source ref are required")
	}
	return nil
}

// ForecastDirection is the signed sense of actual minus forecast.
type ForecastDirection string

// The forecast directions.
const (
	ForecastOver  ForecastDirection = "OVER"
	ForecastUnder ForecastDirection = "UNDER"
	ForecastExact ForecastDirection = "EXACT"
)

// ForecastReconciliation is the closed comparison record.
type ForecastReconciliation struct {
	SignalID        string
	ActualRef       string
	Interval        string
	Forecast        values.Quantity
	Actual          values.Quantity
	Direction       ForecastDirection
	Delta           values.Quantity
	ModelVersion    string
	ForecastSource  string
	ActualSource    string
	CanonicalDigest string
}

// Validate implements validation.
func (r ForecastReconciliation) Validate() error {
	if strings.TrimSpace(r.SignalID) == "" || strings.TrimSpace(r.ActualRef) == "" || strings.TrimSpace(r.Interval) == "" {
		return errors.New("demand reconciliation binding is required")
	}
	for name, quantity := range map[string]values.Quantity{"forecast": r.Forecast, "actual": r.Actual, "delta": r.Delta} {
		if err := quantity.Validate(); err != nil {
			return fmt.Errorf("demand reconciliation %s: %w", name, err)
		}
	}
	switch r.Direction {
	case ForecastOver, ForecastUnder, ForecastExact:
	default:
		return fmt.Errorf("demand reconciliation direction %q is not declared", r.Direction)
	}
	if r.Direction == ForecastExact && r.Delta.Value().Sign() != 0 {
		return errors.New("demand reconciliation exact direction needs a zero delta")
	}
	if r.Direction != ForecastExact && r.Delta.Value().Sign() == 0 {
		return errors.New("demand reconciliation nonzero direction needs a nonzero delta")
	}
	if strings.TrimSpace(r.ModelVersion) == "" || strings.TrimSpace(r.ForecastSource) == "" || strings.TrimSpace(r.ActualSource) == "" {
		return errors.New("demand reconciliation versions are required")
	}
	if r.CanonicalDigest != "" && r.CanonicalDigest != r.computedDigest() {
		return errors.New("demand reconciliation digest mismatch")
	}
	return nil
}

func (r ForecastReconciliation) body() []byte {
	w := canonicalbytes.New("hcmnext.domains.demand.ForecastReconciliation", schemaVersion).
		String("signal_id", r.SignalID).String("actual_ref", r.ActualRef).String("interval", r.Interval).
		Value("forecast", r.Forecast).Value("actual", r.Actual).
		String("direction", string(r.Direction)).Value("delta", r.Delta).
		String("model_version", r.ModelVersion).String("forecast_source", r.ForecastSource).
		String("actual_source", r.ActualSource)
	b, err := w.Bytes()
	if err != nil {
		return nil
	}
	return b
}

func (r ForecastReconciliation) computedDigest() string {
	raw := r.body()
	if raw == nil {
		return ""
	}
	return canonicalbytes.Digest(raw)
}

// Canonical returns the canonical bytes of a valid reconciliation.
func (r ForecastReconciliation) Canonical() []byte {
	if r.Validate() != nil {
		return nil
	}
	return r.body()
}

// ReconcileForecast closes one forecast signal against measured
// actuals. It is pure: inputs are never mutated and nothing is
// persisted.
func ReconcileForecast(signal DemandSignal, actual DemandActual) (ForecastReconciliation, error) {
	if err := signal.Validate(); err != nil {
		return ForecastReconciliation{}, fmt.Errorf("demand reconcile forecast: %w", err)
	}
	if err := actual.Validate(); err != nil {
		return ForecastReconciliation{}, fmt.Errorf("demand reconcile actual: %w", err)
	}
	if !actual.Complete {
		return ForecastReconciliation{}, errors.New("demand reconcile: partial actuals cannot close the comparison")
	}
	if actualScope(actual) != demandScope(signal.Location, signal.OrgUnit) || actualTarget(actual) != demandTarget(signal) {
		return ForecastReconciliation{}, errors.New("demand reconcile: actual scope or target differs from forecast")
	}
	if signal.Quantity.Unit() != actual.Quantity.Unit() {
		return ForecastReconciliation{}, errors.New("demand reconcile: actual unit differs from forecast")
	}
	if !bytes.Equal(actual.Period.Canonical(), signal.Work.Canonical()) {
		return ForecastReconciliation{}, errors.New("demand reconcile: actual period differs from forecast interval")
	}
	delta, direction, err := forecastDelta(signal.Quantity, actual.Quantity)
	if err != nil {
		return ForecastReconciliation{}, err
	}
	reconciliation := ForecastReconciliation{
		SignalID: signal.SignalID, ActualRef: actual.ActualRef, Interval: signal.Work.String(),
		Forecast: signal.Quantity, Actual: actual.Quantity, Direction: direction, Delta: delta,
		ModelVersion: signal.Version, ForecastSource: sourceRef(signal.Source, signal.SourceRef),
		ActualSource: sourceRef(actual.Source, actual.SourceRef),
	}
	reconciliation.CanonicalDigest = reconciliation.computedDigest()
	if err := reconciliation.Validate(); err != nil {
		return ForecastReconciliation{}, err
	}
	return reconciliation, nil
}

func sourceRef(source, ref string) string {
	if strings.TrimSpace(ref) != "" {
		return ref
	}
	return source
}

func forecastDelta(forecast, actual values.Quantity) (values.Quantity, ForecastDirection, error) {
	switch cmp := actual.Value().Cmp(forecast.Value()); {
	case cmp == 0:
		zero, err := zeroLike(forecast)
		if err != nil {
			return values.Quantity{}, "", err
		}
		return zero, ForecastExact, nil
	case cmp > 0:
		delta, err := actual.Sub(forecast)
		if err != nil {
			return values.Quantity{}, "", fmt.Errorf("demand reconcile delta: %w", err)
		}
		return delta, ForecastOver, nil
	default:
		delta, err := forecast.Sub(actual)
		if err != nil {
			return values.Quantity{}, "", fmt.Errorf("demand reconcile delta: %w", err)
		}
		return delta, ForecastUnder, nil
	}
}
