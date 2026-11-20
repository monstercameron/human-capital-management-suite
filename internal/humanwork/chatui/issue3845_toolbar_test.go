//go:build !(js && wasm)

package chatui

import (
	"strconv"
	"strings"
	"testing"
)

// TestIssue3845_HoverToolbarStaysInsideItsOwnRow: the hover bar of a message was
// lifted a full bar height above the row's top edge (translateY(-100%)), so it
// reached up over the last line of the message above. Every row's bar now sits
// inside its own row, and the author line keeps room for it.
func TestIssue3845_HoverToolbarStaysInsideItsOwnRow(t *testing.T) {
	const bar = ".chat-workspace .message-list .message .message-actions"
	if got := chatbugCascadeValue(Stylesheet, bar, "transform"); got != "none" {
		t.Fatalf("the bar is moved out of its row (transform=%q): it covers the message above", got)
	}
	top := chatbugCascadeValue(Stylesheet, bar, "top")
	px, err := strconv.Atoi(strings.TrimSuffix(top, "px"))
	if err != nil || px < 0 {
		t.Fatalf("the bar starts above its row's top edge (top=%q)", top)
	}
	// Inside the row the bar sits over the author line's end; the line keeps the
	// room, whole width in a wide column and the cut-down bar's in a narrow one.
	const meta = ".chat-workspace .message-list .message .message-meta"
	if !strings.Contains(ChatBug076Styles, "@media(min-width:768px) and (pointer:fine){\n"+bar+"{top:4px;transform:none}\n"+meta+"{padding-inline-end:196px}") {
		t.Fatalf("the author line keeps no room for the bar inside the row: %s", ChatBug076Styles)
	}
	if !strings.Contains(ChatBug076Styles, "@container chatmain (max-width:560px){\n"+meta+"{padding-inline-end:84px}") {
		t.Fatalf("a narrow column does not shrink the room kept for the cut-down bar: %s", ChatBug076Styles)
	}
	// Logical properties only: the room is kept on the bar's side in right-to-left
	// languages too.
	if strings.Contains(ChatBug076Styles, "padding-right") || strings.Contains(ChatBug076Styles, "translateY(-100%)") {
		t.Fatal("the bar styles still lift the bar out of its row or use a physical side")
	}
}
