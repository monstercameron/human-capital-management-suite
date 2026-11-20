package chatui

import (
	"os"
	"regexp"
	"testing"
)

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

// The Close thread button must put focus back on the opener, as Escape does.
func TestIssue3722_CloseThreadButtonRestoresOpenerFocus(t *testing.T) {
	if got := issue3883CloseLayerKind("close-thread"); got != "thread" {
		t.Fatalf("Close thread restores focus to layer %q instead of %q", got, "thread")
	}
	raw, err := os.ReadFile("render.go")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?s)m\.act\(action, id\)\s*(//[^\n]*\n\s*)*if kind := issue3883CloseLayerKind\(action\); kind != "" \{\s*restoreChatLayerFocus\(kind\)`).Match(raw) {
		t.Errorf("the click path does not restore focus after closing the thread panel")
	}
}
