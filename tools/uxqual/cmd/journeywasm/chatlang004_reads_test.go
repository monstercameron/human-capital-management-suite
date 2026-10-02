package main

import (
	"sort"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATLANG_004_Reads: a message whose translation is on its way, or
// did not arrive yet, is asked about again; one that is settled is not.
func TestTodo_CHATLANG_004_Reads(t *testing.T) {
	translate := []chatrender.Kind{chatrender.Translate}
	selections := map[string]chatui.ReaderSelection{
		"pending":  {Mark: chatrender.Mark{State: "pending", CanShowOriginal: true, Wanted: translate}},
		"late":     {Mark: chatrender.Mark{State: "fallback", CanShowOriginal: true, Wanted: translate}},
		"ready":    {Mark: chatrender.Mark{State: "ready", Kinds: translate}},
		"original": {Mark: chatrender.Mark{State: "original"}},
		// Not asked for: a reworded view that is pending is not this feature's.
		"reword":   {Mark: chatrender.Mark{State: "pending", Wanted: []chatrender.Kind{chatrender.Reword}}},
		"withheld": {Mark: chatrender.Mark{State: "unavailable"}},
	}
	got := chatlang004Unsettled(selections)
	sort.Strings(got)
	if len(got) != 2 || got[0] != "late" || got[1] != "pending" {
		t.Fatalf("unsettled = %v", got)
	}
	if len(chatlang004Unsettled(nil)) != 0 {
		t.Fatal("nothing is unsettled when nothing was read")
	}

	m := chatui.Model{CurrentTenantID: "t", CurrentUser: "u", SelectedID: "room", Conversations: []chatui.Conversation{{ID: "room", MemberCount: 4}}}
	base := chatlang004AudienceKey(m, 0)
	if chatlang004AudienceKey(m, 0) != base {
		t.Fatal("the audience key is not stable")
	}
	changed := []chatui.Model{m, m, m}
	changed[0].SelectedID = "other"
	changed[1].Conversations = []chatui.Conversation{{ID: "room", MemberCount: 5}}
	changed[2].CurrentUser = "someone"
	for i, next := range changed {
		if chatlang004AudienceKey(next, 0) == base {
			t.Fatalf("change %d does not change the key", i)
		}
	}
	if chatlang004AudienceKey(m, 1) == base {
		t.Fatal("a settings change does not change the key")
	}
}
