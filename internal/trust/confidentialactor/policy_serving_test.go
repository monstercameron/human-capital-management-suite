package confidentialactor

import "testing"

func TestTodo_ANON_001_Served(t *testing.T) {
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("served confidential-actor contract: %v", err)
	}
}
