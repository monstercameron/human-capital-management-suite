package privacy

import "testing"

func TestServingContractValidation(t *testing.T) {
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
