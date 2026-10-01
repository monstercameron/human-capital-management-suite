package career

import "testing"

func TestTodo_CAREER_001_Served(t *testing.T) {
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("career serving contract: %v", err)
	}
}
