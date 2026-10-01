package workspace

import "testing"

func TestTodo_ALIGN_063_Served(t *testing.T) {
	if err := validateJourneyRendererServingContract(); err != nil {
		t.Fatalf("validateJourneyRendererServingContract: %v", err)
	}
}
