package payinput

import "testing"

func TestTodo_PAYINPUT_001_Served(t *testing.T) {
	if ServingContractID != "hcmnext.conformance.payroll-inputs/v1" {
		t.Fatalf("ServingContractID = %q", ServingContractID)
	}
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}

func TestTodo_PAYINPUT_002_Served(t *testing.T) {
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}

func TestTodo_PAYINPUT_003_Served(t *testing.T) {
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
