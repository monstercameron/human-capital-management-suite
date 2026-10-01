package egress

import "testing"

func TestTodo_EDGE_005_ServedContract(t *testing.T) {
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
