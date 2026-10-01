package pseudonym

import "testing"

func TestTodo_ANON_002_Served(t *testing.T) {
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("served pseudonym contract: %v", err)
	}
}

func TestTodo_ANON_003_Served(t *testing.T) {
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("served escrow contract: %v", err)
	}
}

func TestTodo_ANON_004_Served(t *testing.T) {
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("served revelation contract: %v", err)
	}
}
