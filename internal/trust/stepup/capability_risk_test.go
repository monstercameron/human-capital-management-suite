package stepup_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/stepup"
)

// TestTodo_INTAPI_003_CapabilityRisk is the RED test for INTAPI-003's
// step-up leg: writes of high risk class must require step-up or dual
// approval, which starts with naming which capability risk classes are high.
// Today no mapping from a capability's declared risk class onto the step-up
// risk tiers exists, so a high-risk write and a routine read are
// indistinguishable to the gate.
func TestTodo_INTAPI_003_CapabilityRisk(t *testing.T) {
	for _, tc := range []struct {
		class string
		want  stepup.Risk
	}{
		{"LOW", stepup.RiskRoutine},
		{"MEDIUM", stepup.RiskElevated},
		{"HIGH", stepup.RiskCritical},
		{"CRITICAL", stepup.RiskCritical},
		{"high", stepup.RiskCritical},
		{"", stepup.RiskUnspecified},
		{"bogus", stepup.RiskUnspecified},
	} {
		if got := stepup.RiskForCapabilityClass(tc.class); got != tc.want {
			t.Errorf("RiskForCapabilityClass(%q) = %s, want %s", tc.class, got, tc.want)
		}
	}
}
