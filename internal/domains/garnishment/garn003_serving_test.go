package garnishment

import "testing"

// TestTodo_GARN_003_Served proves the disposable-earnings contract is linked
// to a pure serving seam, not only reachable from its unit tests.
func TestTodo_GARN_003_Served(t *testing.T) {
	if ServingContractID != "hcmnext.conformance.garnishment-limit/v1" {
		t.Fatalf("ServingContractID = %q", ServingContractID)
	}
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("garnishment serving contract: %v", err)
	}
}
