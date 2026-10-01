package succession

import "testing"

func TestTodo_SUCCESSION_001_ServedPath(t *testing.T) {
	if ServingContractID != "hcmnext.conformance.succession/v1" {
		t.Fatalf("ServingContractID = %q", ServingContractID)
	}
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
