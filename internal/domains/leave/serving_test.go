package leave

import "testing"

func TestValidateServingContract(t *testing.T) {
	if ServingContractID != "hcmnext.conformance.leave/v1" {
		t.Fatalf("ServingContractID = %q", ServingContractID)
	}
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
