package demoworkforce

import "testing"

// TestTodo_CHATSEED_001 runs the seed-completeness proofs under the todo's
// name: every demo employee carries every field Person details can show, the
// manager chain has no gap or cycle, and no two people share a photograph.
func TestTodo_CHATSEED_001(t *testing.T) {
	t.Run("every employee complete", TestDemoSeed_EveryEmployeeComplete)
	t.Run("no image is shared", TestDirectoryPhotos_NoImageIsShared)
	t.Run("roster rows are unchanged", TestDirectoryPhotos_RosterRowsAreUnchanged)
}
