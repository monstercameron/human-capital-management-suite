package stepup

import "strings"

// RiskForCapabilityClass maps a capability definition's declared risk class
// onto the step-up risk tiers. Capability authors declare LOW, MEDIUM, HIGH
// or CRITICAL; anything else — empty, misspelled, a risk vocabulary from
// another layer — maps to RiskUnspecified, which every obligation evaluation
// treats as "required and unmet" rather than as a silent pass. Matching is
// case-insensitive so a definition's casing never changes its gate.
func RiskForCapabilityClass(class string) Risk {
	switch strings.ToUpper(strings.TrimSpace(class)) {
	case "LOW":
		return RiskRoutine
	case "MEDIUM":
		return RiskElevated
	case "HIGH", "CRITICAL":
		return RiskCritical
	default:
		return RiskUnspecified
	}
}
