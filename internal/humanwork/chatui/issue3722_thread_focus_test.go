package chatui

import "testing"

// Issue 3722: focus returns to the control that opened the thread when the
// thread panel closes. The thread-count link ("stats") opens the thread, so it
// must be the opener the thread layer restores focus to, not a details layer.
func TestIssue3722_ThreadCountLinkRestoresFocusToThreadLayer(t *testing.T) {
	for _, action := range []string{"reply", "stats"} {
		if got := chatLayerKind(action); got != "thread" {
			t.Fatalf("%s restores focus to layer %q instead of %q", action, got, "thread")
		}
	}
}
