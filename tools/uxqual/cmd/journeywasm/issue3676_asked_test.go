package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func TestIssue3676_AskedMessagesAreNotAskedAgain(t *testing.T) {
	var asked issue3676Asked
	page := make([]chatui.Message, 0, 35)
	for i := 0; i < 35; i++ {
		page = append(page, chatui.Message{ID: "m" + string(rune('a'+i%26)) + string(rune('a'+i/26)), Revision: 1})
	}
	page = append(page, page[0]) // duplicate id (thread parent also in timeline)
	first := asked.Unasked("room", page)
	if len(first) != 35 {
		t.Fatalf("first read asks for %d messages, want 35 once each", len(first))
	}
	asked.Answered("room", first)
	if again := asked.Unasked("room", page); len(again) != 0 {
		t.Fatalf("an unrelated rerender asked for %d messages again", len(again))
	}
	grown := append(append([]chatui.Message{}, page...), chatui.Message{ID: "new", Revision: 1})
	if next := asked.Unasked("room", grown); len(next) != 1 || next[0].ID != "new" {
		t.Fatalf("a new message should ask only for itself: %v", next)
	}
	edited := append([]chatui.Message{}, page...)
	edited[3].Revision = 2
	if next := asked.Unasked("room", edited); len(next) != 1 || next[0].ID != page[3].ID {
		t.Fatalf("an edited message should be asked again: %v", next)
	}
	if other := asked.Unasked("other", page); len(other) != 35 {
		t.Fatalf("another conversation shares answers: %d", len(other))
	}
	// A failed read records nothing, so the retry asks again.
	var failed issue3676Asked
	if len(failed.Unasked("room", page)) != 35 || len(failed.Unasked("room", page)) != 35 {
		t.Fatal("unanswered messages must stay to be asked")
	}
	// An answer that was not kept (the person left the conversation) is asked again.
	asked.Forget("room", first[:5])
	if again := asked.Unasked("room", page); len(again) != 5 {
		t.Fatalf("forgotten answers asked %d messages, want 5", len(again))
	}
	asked.Reset()
	if len(asked.Unasked("room", page)) != 35 {
		t.Fatal("reset did not forget")
	}
}

func TestIssue3676_MergeLocationsKeepsUnaskedMessages(t *testing.T) {
	current := map[string][]chatui.ChatmapEmbed{"old": {{}}, "edited": {{}}}
	got := issue3676MergeLocations(current, []chatui.Message{{ID: "edited"}, {ID: "new"}}, map[string][]chatui.ChatmapEmbed{"new": {{}}})
	if len(got["old"]) != 1 || len(got["new"]) != 1 {
		t.Fatalf("known or new locations missing: %v", got)
	}
	if _, ok := got["edited"]; ok {
		t.Fatal("an asked message with no answer kept a stale location")
	}
	if len(current) != 2 {
		t.Fatal("the current map was modified")
	}
}
