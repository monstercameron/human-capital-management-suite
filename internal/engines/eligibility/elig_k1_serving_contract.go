package eligibility

import "fmt"

// ValidateServingContract verifies the immutable contract that the served
// intent composition relies on. It is deliberately read-only: linking this
// check into the serving composition does not evaluate a request or create a
// domain effect.
func ValidateServingContract() error {
	if Version() <= 0 {
		return fmt.Errorf("eligibility: serving contract version must be positive")
	}
	if requestSchema == "" || resultSchema == "" {
		return fmt.Errorf("eligibility: serving contract schemas are required")
	}
	return nil
}
