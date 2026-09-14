package simcontract_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/simcontract"
)

// TestTodo_PROMOUX_009_Mutation_RejectsUnknownSeverity kills the mutant that
// treats every non-zero Severity value as a valid canonical finding. Unknown
// values must not become executable findings with the misleading
// SEVERITY_UNSPECIFIED wire token.
func TestTodo_PROMOUX_009_Mutation_RejectsUnknownSeverity(t *testing.T) {
	for _, severity := range []promotion.Severity{
		promotion.SeverityUnspecified,
		promotion.Severity(255),
	} {
		t.Run(severity.String(), func(t *testing.T) {
			if _, err := simcontract.NewFinding(
				"budget.observation", "rewards", severity, "budget",
				"promotion.proposal", "budget was observed", "rewards",
			); err == nil {
				t.Fatalf("NewFinding accepted invalid severity %d", severity)
			}
		})
	}
}
