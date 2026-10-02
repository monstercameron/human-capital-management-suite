package main

import (
	"os"
	"strings"
	"testing"
)

// TestTodo_CHATBUG_085_Escape holds Escape in the report and removal dialogs:
// it closes the dialog from any control, the listener hears the key before the
// workspace can take it, and focus goes back to the message's More button.
func TestTodo_CHATBUG_085_Escape(t *testing.T) {
	for _, tc := range []struct {
		name                                    string
		dialogOpen, pageOpen, inPage, composing bool
		want                                    string
	}{
		{"a reason radio in the report dialog", true, false, false, false, "dialog"},
		{"the details box of a dialog opened over the Moderation page", true, true, true, false, "dialog"},
		{"the page itself has focus while a dialog is open", true, false, false, false, "dialog"},
		{"the search box of the Moderation page", false, true, true, false, "page"},
		{"the sidebar while the Moderation page shows", false, true, false, false, ""},
		{"nothing of moderation is open", false, false, false, false, ""},
		{"Escape that ends a composition in the details box", true, false, false, true, ""},
	} {
		if got := chatremoveEscapeCloses(tc.dialogOpen, tc.pageOpen, tc.inPage, tc.composing); got != tc.want {
			t.Errorf("%s: Escape closes %q, want %q", tc.name, got, tc.want)
		}
	}
	if !chatbug085AwaitsMore(true, false) || !chatbug085AwaitsMore(false, true) || chatbug085AwaitsMore(false, false) {
		t.Error("focus moves on to More exactly when the row it was handed to is active: focus shows, or a phone")
	}

	// The listener must be the first to hear the key: on the window, capturing,
	// and it must stop the key so the workspace does not close what is behind.
	source, err := os.ReadFile("chatremove_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, want := range []string{
		`js.Global().Call("addEventListener", "keydown", b.escape, true)`,
		`js.Global().Call("removeEventListener", "keydown", b.escape, true)`,
		`chatremoveEscapeCloses(`,
		`event.Call("stopImmediatePropagation")`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("chatremove_wasm.go misses %s", want)
		}
	}
	overlay, err := os.ReadFile("chatmod005_overlay_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(overlay), "chatbug085FocusReturn(opener)") {
		t.Error("closing a dialog does not hand focus to the message's More button")
	}
	dialog, err := os.ReadFile("chatbug085_dialog_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`[data-action="menu"]`, "MutationObserver", "chatbug085AwaitsMore("} {
		if !strings.Contains(string(dialog), want) {
			t.Errorf("chatbug085_dialog_wasm.go misses %s", want)
		}
	}
	for _, banned := range []string{"setTimeout", "requestAnimationFrame"} {
		if strings.Contains(string(dialog), banned) {
			t.Errorf("the focus return waits on %s, which a throttled window never runs", banned)
		}
	}
}
