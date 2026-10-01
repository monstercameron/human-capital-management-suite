package productcorrelation_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/productcorrelation"
)

func TestTodo_ALIGN_053_Served(t *testing.T) {
	if err := productcorrelation.ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
