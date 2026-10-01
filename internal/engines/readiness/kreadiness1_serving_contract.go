package readiness

import "fmt"

// ValidateServingContract verifies the immutable, bounded contract that the
// serving composition relies on. It is deliberately read-only: linking this
// check into the application does not resolve evidence or create a domain
// effect.
func ValidateServingContract() error {
	if Version() <= 0 {
		return fmt.Errorf("readiness: serving contract version must be positive")
	}
	for _, status := range []ReadinessStatus{
		StatusReady, StatusConditional, StatusNotReady, StatusUnknown,
	} {
		if !status.Valid() {
			return fmt.Errorf("readiness: invalid readiness status %q", status)
		}
	}
	for _, status := range []ResolutionStatus{
		ResolutionSatisfied, ResolutionUnsatisfied, ResolutionConditional, ResolutionUnknown,
	} {
		if !status.Valid() {
			return fmt.Errorf("readiness: invalid resolution status %q", status)
		}
	}
	return nil
}
