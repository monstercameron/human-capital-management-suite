package accessdrift

import "testing"

func TestTodo_ACCESS_004_ServedPath(t *testing.T) {
	if ServingContractID == "" {
		t.Fatal("access-drift serving contract has no stable identity")
	}
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
