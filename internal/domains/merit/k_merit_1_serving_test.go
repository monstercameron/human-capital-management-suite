package merit

import "testing"

func TestTodo_MERIT_001_Served(t *testing.T) {
	if ServingContractID != "hcmnext.conformance.merit/v1" {
		t.Fatalf("ServingContractID = %q", ServingContractID)
	}
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
