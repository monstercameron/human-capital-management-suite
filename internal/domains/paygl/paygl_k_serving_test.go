package paygl

import "testing"

func TestTodo_PAYGL_001_Served(t *testing.T) {
	if ServingContractID == "" {
		t.Fatal("serving contract id is required")
	}
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
