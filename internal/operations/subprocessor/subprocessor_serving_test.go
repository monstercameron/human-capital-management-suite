package subprocessor

import "testing"

func TestTodo_SUBPROCESSOR_001_ServedPath(t *testing.T) {
	contract := Contract()
	if contract.ID != ServingContractID || contract.Version != Version() || contract.Owner != CapabilityOwner {
		t.Fatalf("contract metadata=%+v", contract)
	}
	if contract.Owner != "GOVERNANCE" {
		t.Fatalf("accountable capability owner=%q", contract.Owner)
	}
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("ValidateServingContract: %v", err)
	}
}
