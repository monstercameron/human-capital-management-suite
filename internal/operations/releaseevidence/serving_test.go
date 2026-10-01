package releaseevidence_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/releaseevidence"
)

func TestTodo_ALIGN_060_Served(t *testing.T) {
	if err := releaseevidence.ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
