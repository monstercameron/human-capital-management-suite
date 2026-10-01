package dsr

import "fmt"

// ValidateServingContract checks the closed PRIV-006 vocabulary that a
// serving composition relies on. Keeping this check with the semantic owner
// makes a serving cell depend on the same contract the resolver enforces,
// rather than treating the package as test-only library code.
func ValidateServingContract() error {
	classes := AllCopyClasses()
	if len(classes) == 0 {
		return fmt.Errorf("dsr: serving contract declares no copy classes")
	}
	seen := make(map[CopyClass]struct{}, len(classes))
	for _, class := range classes {
		if _, duplicate := seen[class]; duplicate {
			return fmt.Errorf("dsr: serving contract repeats copy class %q", class)
		}
		if err := class.Validate(); err != nil {
			return fmt.Errorf("dsr: serving contract copy class %q: %w", class, err)
		}
		seen[class] = struct{}{}
	}
	for _, outcome := range []ItemOutcome{
		OutcomeFulfill, OutcomePartial, OutcomeDeny,
		OutcomeRestrict, OutcomeRetain, OutcomeAnonymize,
	} {
		if err := outcome.Validate(); err != nil {
			return fmt.Errorf("dsr: serving contract outcome %q: %w", outcome, err)
		}
	}
	return nil
}
