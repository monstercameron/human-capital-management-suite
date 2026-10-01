package refdata

import "testing"

func TestTodo_REFDATA_001_Conformance_Serving(t *testing.T) {
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
