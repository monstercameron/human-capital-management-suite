package authorizedhealth_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/authorizedhealth"
)

func TestTodo_ALIGN_051_Served(t *testing.T) {
	if err := authorizedhealth.ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
