package journey

import "testing"

// TestTodo_CHATSEED_001_Integration runs the directory enrichment proofs under
// the todo's name: the served directory answers with the department's name,
// the work email and phone, and a photograph for a demo person whose stored
// row has none, and it can be applied again without changing a stored row.
func TestTodo_CHATSEED_001_Integration(t *testing.T) {
	t.Run("contact fields and department name", TestEnrichDemoDirectory)
	t.Run("photographs", TestEnrichDemoDirectoryPhotos)
}
