package conformance_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/transport/conformance"
)

func TestTodo_ALIGN_061_Served(t *testing.T) {
	if err := conformance.ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
