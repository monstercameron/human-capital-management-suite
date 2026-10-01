package employeerelations

import "testing"

func TestTodo_ER_001_Served(t *testing.T) {
	if ServingContractID == "" {
		t.Fatal("serving contract id is required")
	}
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}

func TestTodo_ER_002_Served(t *testing.T) {
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
