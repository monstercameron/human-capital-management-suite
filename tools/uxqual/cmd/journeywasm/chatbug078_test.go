package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestTodo_CHATBUG_078 holds the client's half of the Saved fixes: a saved
// message that was deleted cannot be done, so it leaves To do, its count and
// the sidebar number, and is listed under All alone, where it keeps its remove
// control.
func TestTodo_CHATBUG_078(t *testing.T) {
	page := chat.SavedPage{Items: []chat.SavedItem{
		{PostID: "live", State: chat.SavedTodo, Availability: "readable"},
		{PostID: "gone", State: chat.SavedTodo, Availability: "deleted"},
		{PostID: "gone-done", State: chat.SavedDone, Availability: "deleted"},
		{PostID: "done", State: chat.SavedDone, Availability: "readable"},
		{PostID: "removed", State: chat.SavedTodo, Availability: "removed"},
	}}
	todo, done, all := chatsaveCounts(page)
	if todo != 2 || done != 1 || all != 5 {
		t.Fatalf("counts todo %d done %d all %d; a deleted message must count under All only", todo, done, all)
	}
	if open, known := chatsaveOpenCount(page, true); !known || open != 2 {
		t.Fatalf("the sidebar number is %d (known %v), want 2", open, known)
	}
	listed := func(tab string) []string {
		ids := []string{}
		for _, item := range page.Items {
			if chatsaveInTab(item, tab) {
				ids = append(ids, item.PostID)
			}
		}
		return ids
	}
	if got := listed("todo"); len(got) != 2 || got[0] != "live" || got[1] != "removed" {
		t.Fatalf("To do lists %v", got)
	}
	if got := listed("done"); len(got) != 1 || got[0] != "done" {
		t.Fatalf("Done lists %v", got)
	}
	if got := listed("all"); len(got) != 5 {
		t.Fatalf("All lists %v, want every saved message", got)
	}
}
