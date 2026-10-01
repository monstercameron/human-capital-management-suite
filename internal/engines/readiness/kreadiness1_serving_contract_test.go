package readiness_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/readiness"
)

func TestTodo_READINESS_001_ServingContract(t *testing.T) {
	if err := readiness.ValidateServingContract(); err != nil {
		t.Fatalf("readiness serving contract: %v", err)
	}
}
