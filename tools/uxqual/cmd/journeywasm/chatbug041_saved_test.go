package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_CHATBUG_041: the Saved row's number is the count of items still to do
// from a whole read of the list, and no number at all until that read has
// landed. A save made before the read returned leaves a one-item list that must
// never be counted: it showed 1 (or 3) and then changed to the real count by
// itself.
func TestTodo_CHATBUG_041(t *testing.T) {
	todo := func(n int) []chat.SavedItem {
		items := make([]chat.SavedItem, n)
		for i := range items {
			items[i] = chat.SavedItem{PostID: string(rune('a' + i)), State: chat.SavedTodo}
		}
		return items
	}
	// Before the first read: nothing is known, whatever the list holds.
	for name, page := range map[string]chat.SavedPage{"empty": {}, "a local save": {Items: todo(1)}, "a partial list": {Items: todo(3)}} {
		if n, known := chatsaveOpenCount(page, false); known || n != 0 {
			t.Errorf("%s before the first read: the count is %d, known=%v; it must be unknown", name, n, known)
		}
	}
	// After a whole read: the items still to do, from that read.
	whole := chat.SavedPage{Items: append(todo(4), chat.SavedItem{PostID: "z", State: chat.SavedDone})}
	if n, known := chatsaveOpenCount(whole, true); !known || n != 4 {
		t.Errorf("after the read: %d known=%v, want 4 known", n, known)
	}
	if n, known := chatsaveOpenCount(chat.SavedPage{}, true); !known || n != 0 {
		t.Errorf("a list read as empty is known to be empty: %d %v", n, known)
	}
	// The number does not change between the first read and a later one that
	// holds the same list.
	again := chat.SavedPage{Items: append(todo(4), chat.SavedItem{PostID: "z", State: chat.SavedDone})}
	first, _ := chatsaveOpenCount(whole, true)
	second, _ := chatsaveOpenCount(again, true)
	if first != second {
		t.Errorf("the count changed by itself: %d then %d", first, second)
	}
	// Marking one done and ticking it off again move the number by one each way.
	page := chatsaveApplyLocal(whole, "", chatsaveCommand{Action: "done", ConversationID: "", PostID: "a"}, nil)
	if n, _ := chatsaveOpenCount(page, true); n != 3 {
		t.Errorf("after one is done: %d", n)
	}
	page = chatsaveApplyLocal(page, "", chatsaveCommand{Action: "reopen", ConversationID: "", PostID: "a"}, nil)
	if n, _ := chatsaveOpenCount(page, true); n != 4 {
		t.Errorf("after it is reopened: %d", n)
	}
}
