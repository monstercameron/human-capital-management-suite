package eastwest

import "testing"

func TestTodo_EDGE_006_ServedContract(t *testing.T) {
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
