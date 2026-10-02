package application

import "testing"

// TestTodo_CHATBUG_062 pins the addresses the filter panel asks for. The list of
// filters is read from the bare address; the served assembly routed only the
// addresses under it, so the panel got "not found" and told the administrator
// that filters were not available.
func TestTodo_CHATBUG_062(t *testing.T) {
	for _, path := range []string{
		ChatFiltersPath,
		ChatFiltersPath + "/enablements",
		ChatFiltersPath + "/hits",
		ChatFiltersPath + "/versions",
		ChatFiltersPath + "/enable",
		ChatFiltersPath + "/disable",
		ChatFiltersPath + "/try",
	} {
		if !agentServedPath(path) {
			t.Errorf("the served assembly does not route %s", path)
		}
	}
	if agentServedPath(ChatFiltersPath + "x") {
		t.Errorf("an address that only starts like the filter address is routed")
	}
}
