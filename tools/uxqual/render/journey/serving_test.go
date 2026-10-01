package journey_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

func TestTodo_ALIGN_063_Served(t *testing.T) {
	if err := journey.ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
