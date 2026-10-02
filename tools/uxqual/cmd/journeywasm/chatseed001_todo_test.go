package main

import "testing"

// TestTodo_CHATSEED_001_Browser runs the Chat client proofs under the todo's
// name: Person details carries the department's name and the work contact, and
// the people who had no photograph are drawn with an image, not initials.
func TestTodo_CHATSEED_001_Browser(t *testing.T) {
	t.Run("department name and work contact", TestChatPersonDetailsCarryDepartmentNameAndWorkContact)
	t.Run("photographs", TestChatPhotosSeededPeopleAreDrawnWithAnImage)
}
