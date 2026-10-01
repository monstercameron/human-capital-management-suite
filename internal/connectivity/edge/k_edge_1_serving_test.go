package edge

import "testing"

func TestTodo_EDGE_008_009_ServedContract(t *testing.T) {
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
