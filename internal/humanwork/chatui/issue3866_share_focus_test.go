package chatui

import "testing"

func TestIssue3866_ShareReturnSelectorsPreferMenuButtonThenMessage(t *testing.T) {
	got := shareReturnSelectors(`a"b`)
	want := []string{`.message-action[data-action="menu"][data-id="a\"b"]`, `[data-message-id="a\"b"]`}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("selectors = %q", got)
	}
}
