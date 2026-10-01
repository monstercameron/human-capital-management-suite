package workflow

import "testing"

func TestTodo_WFPAGE_002_CatalogMetadataClone(t *testing.T) {
	original := &CatalogMetadata{DisplayName: "New hire", Keywords: []string{"onboarding"}}
	clone := original.Clone()
	clone.Keywords[0] = "changed"
	if original.Keywords[0] != "onboarding" || clone.DisplayName != original.DisplayName {
		t.Fatalf("metadata clone = %+v, original = %+v", clone, original)
	}
}
