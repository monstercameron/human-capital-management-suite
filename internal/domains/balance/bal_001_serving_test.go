package balance

import "testing"

func TestTodo_BAL_001_ServedPath(t *testing.T) {
	if ServingContractID == "" {
		t.Fatal("served contract id is empty")
	}
	if err := ValidateAccumulatorServingContract(); err != nil {
		t.Fatalf("balance serving contract: %v", err)
	}
}
