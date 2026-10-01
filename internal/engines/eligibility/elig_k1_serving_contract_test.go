package eligibility_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/eligibility"
)

func TestTodo_ELIG_001_ServingContract(t *testing.T) {
	if err := eligibility.ValidateServingContract(); err != nil {
		t.Fatalf("eligibility serving contract: %v", err)
	}
}
