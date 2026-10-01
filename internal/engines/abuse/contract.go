package abuse

import "fmt"

// Version reports the abuse engine package's own contract version (the
// ARCH-GO-009 engine contract symbol). It is distinct from the business
// version carried by a SignalDefinition, DetectorDefinition, or
// DetectorVersion: those are governed data the engine validates and
// publishes; this is the shape of the engine contract itself, and only
// changes when this package's exported contract changes incompatibly.
func Version() int { return 1 }

// ValidateServingContract is the read-only link checked by the application
// composition. It keeps the abuse engine on the shipped dependency path and
// verifies the bounded response policy that callers use for step-up, review,
// and temporary containment decisions.
func ValidateServingContract() error {
	if Version() <= 0 {
		return fmt.Errorf("abuse: serving contract version must be positive")
	}
	if _, err := NewTriggerPolicy(
		"serving-abuse-response",
		"1",
		[]TriggerRule{
			{ID: "serving-step-up", MinimumScore: 0, MinimumConfidence: 0, Action: TriggerStepUp, Duration: 1},
			{ID: "serving-review", MinimumScore: 50, MinimumConfidence: 0, Action: TriggerHumanReview},
			{ID: "serving-containment", MinimumScore: 90, MinimumConfidence: 0, Action: TriggerTemporaryContainment, Duration: 1},
		},
		ReviewRoute{Queue: "security-review", CaseType: "abuse-risk", SLA: 1, ReviewerSoD: true},
		1,
	); err != nil {
		return fmt.Errorf("abuse: serving trigger policy: %w", err)
	}
	if InsiderVersion == "" || len(InsiderScenarios) == 0 {
		return fmt.Errorf("abuse: serving insider corpus contract is incomplete")
	}
	return nil
}
