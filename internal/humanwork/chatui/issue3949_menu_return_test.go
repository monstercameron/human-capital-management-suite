package chatui

import (
	"os"
	"strings"
	"testing"
)

func TestIssue3949_MenuReturnTarget(t *testing.T) {
	for _, tc := range []struct {
		trigger, button, row bool
		want                 string
	}{
		{true, true, true, "trigger"},
		{false, true, true, "button"},
		// The case of the issue: More is not drawn because the row is not active.
		{false, false, true, "row"},
		{false, false, false, ""},
	} {
		if got := MenuReturnTarget(tc.trigger, tc.button, tc.row); got != tc.want {
			t.Errorf("MenuReturnTarget(%v,%v,%v) = %q, want %q", tc.trigger, tc.button, tc.row, got, tc.want)
		}
	}
	if scope, id := MenuRowScope("thread:p1"); scope != ".thread-pane" || id != "p1" {
		t.Errorf("thread menu scope = %q %q", scope, id)
	}
	if scope, id := MenuRowScope("p2"); scope != ".message-list" || id != "p2" {
		t.Errorf("list menu scope = %q %q", scope, id)
	}
}

// The browser half cannot run here; pin that closing a menu falls back to the
// message row when its More button is not drawn, rather than dropping focus.
func TestIssue3949_RestoreMenuFocusFallsBackToRow(t *testing.T) {
	source, err := os.ReadFile("events_js.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	start := strings.Index(text, "func restoreMessageMenuFocus(")
	end := strings.Index(text, "func moveMessageMenuFocus(")
	if start < 0 || end < start {
		t.Fatal("restoreMessageMenuFocus not found")
	}
	if !strings.Contains(text[start:end], "focusMessageRowForMenu(id)") {
		t.Error("restoreMessageMenuFocus drops focus when the More button is not drawn")
	}
	helper, err := os.ReadFile("issue3949_menu_return_js.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"MenuReturnTarget(", "MenuRowScope(", "MutationObserver"} {
		if !strings.Contains(string(helper), want) {
			t.Errorf("issue3949_menu_return_js.go misses %s", want)
		}
	}
	for _, banned := range []string{"setTimeout", "requestAnimationFrame"} {
		if strings.Contains(string(helper), banned) {
			t.Errorf("the focus return waits on %s", banned)
		}
	}
}
