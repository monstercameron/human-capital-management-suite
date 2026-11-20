package chatui

import (
	"os"
	"regexp"
	"testing"
)

// Issue 3883: Close details returns focus to the control that opened the panel.
// Every control that opens it is a details-layer opener, so its press is
// remembered, and the Close details press restores it.
func TestIssue3883_EveryDetailsOpenerIsRemembered(t *testing.T) {
	for _, action := range []string{"details", "header-details", "header-pinned", "header-members", "header-map", "agents-here"} {
		if got := chatLayerKind(action); got != "details" {
			t.Errorf("%s is remembered as layer %q, so Close details cannot return focus to it", action, got)
		}
	}
}

func TestIssue3883_CloseDetailsRestoresOpenerFocus(t *testing.T) {
	if got := issue3883CloseLayerKind("close-details"); got != "details" {
		t.Fatalf("Close details restores focus to layer %q", got)
	}
	if got := issue3883CloseLayerKind("reply"); got != "" {
		t.Errorf("another action restores focus to layer %q", got)
	}
	// The click path must call it right after the action ran.
	raw, err := os.ReadFile("render.go")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?s)m\.act\(action, id\)\s*(//[^\n]*\n\s*)*if kind := issue3883CloseLayerKind\(action\); kind != "" \{\s*restoreChatLayerFocus\(kind\)`).Match(raw) {
		t.Errorf("the click path does not restore focus after closing the details panel")
	}
}
