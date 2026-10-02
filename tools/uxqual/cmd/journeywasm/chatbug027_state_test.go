package main

import (
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_027_ThreadCloseClosesItsOverlays: closing the thread pane
// closed the pane and nothing it had opened. A reaction picker started on a
// reply stayed drawn over the page with no message left to anchor to.
func TestTodo_CHATBUG_027_ThreadCloseClosesItsOverlays(t *testing.T) {
	raw, err := os.ReadFile("chat_wasm.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "CloseThread: func() {")
	if start < 0 {
		t.Fatal("the CloseThread callback moved")
	}
	end := strings.Index(source[start:], "refreshChatRoute()")
	body := source[start : start+end]
	clearAt := strings.Index(body, "chatui.ClearThreadOverlays(model)")
	resetAt := strings.Index(body, "model.ShowThread, model.ThreadParentID, model.ThreadMessages = false")
	if clearAt < 0 || resetAt < 0 || clearAt > resetAt {
		t.Fatalf("CloseThread must clear the thread's picker and menu while it still knows the thread's messages (clear at %d, reset at %d)", clearAt, resetAt)
	}

	// The state change itself, as the callback applies it.
	model := chatui.Model{
		ShowThread: true, ThreadParentID: "question",
		ThreadMessages: []chatui.Message{{ID: "reply"}},
		PickerID:       "reply", MenuID: "thread:reply",
	}
	chatui.ClearThreadOverlays(&model)
	model.ShowThread, model.ThreadParentID, model.ThreadMessages = false, "", nil
	if model.PickerID != "" || model.MenuID != "" || model.ShowThread {
		t.Fatalf("thread closed with its overlays still open: %+v", model)
	}
}
