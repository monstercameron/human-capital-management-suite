package dsr

import "testing"

func TestServingContractValidation(t *testing.T) {
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
	if got, want := len(AllCopyClasses()), 11; got != want {
		t.Fatalf("serving copy classes = %d, want %d", got, want)
	}
}
