package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATUX_051_Integration opens the "#" list inside both composers
// together with the other suggestion lists: only the channel list opens on a
// "#" token, the field points at it while focus stays in the box, Escape
// closes it, arrows move through it, and sending clears it with the rest.
func TestTodo_CHATUX_051_Integration(t *testing.T) {
	model := func() Model {
		return Model{Locale: "en-US", SelectedID: "general", CurrentUser: "walt", CurrentTenantID: "t",
			Conversations: []Conversation{
				{ID: "general", Name: "general", Kind: PublicChannel, Joined: true, MemberCount: 18},
				{ID: "design", Name: "design-reviews", Kind: PrivateChannel, Joined: true, MemberCount: 4},
			},
			ThreadParentID: "p1",
			ThreadMessages: []Message{{ID: "r1", Author: "Ann Lee", Body: "reply"}},
			Callbacks:      walkCallbacks()}
	}

	t.Run("OnlyTheChannelListOpens", func(t *testing.T) {
		m := model()
		h := handlers{local: localUI{channelSuggest: channelSuggestState{Target: "chat-composer", Query: "d", Start: 0, End: 2, Open: true, Active: 0}}}
		markup := renderNode(t, composer(m, h))
		if !strings.Contains(markup, `id="chat-composer-channel-suggest"`) {
			t.Fatalf("the composer does not draw the open channel list: %s", markup)
		}
		// The other lists keep their closed slots; none claims the field.
		for _, slot := range []string{"mention-slot", "emoji-completion-slot"} {
			if !strings.Contains(markup, slot) {
				t.Errorf("the composer lost the %s beside the open channel list", slot)
			}
		}
		if strings.Contains(markup, "chat-composer-emoji-completion") || strings.Contains(markup, `id="chat-composer-mentions"`) {
			t.Error("another suggestion list opened with the channel list")
		}
		at := strings.Index(markup, `id="chat-composer"`)
		field := markup[strings.LastIndex(markup[:at], "<textarea"):]
		field = field[:strings.Index(field, ">")+1]
		for _, want := range []string{`aria-expanded="true"`, `aria-controls="chat-composer-channel-suggest"`, `aria-activedescendant="chat-composer-channel-option-0"`} {
			if !strings.Contains(field, want) {
				t.Errorf("the field does not point at the open list (%q): %s", want, field)
			}
		}
	})

	t.Run("ThreadComposer", func(t *testing.T) {
		m := model()
		m.ShowThread, m.ThreadParentID = true, "p1"
		m.Messages = []Message{{ID: "p1", Author: "Walt Brennan", Body: "parent"}}
		m.ThreadParent = &m.Messages[0]
		h := handlers{local: localUI{channelSuggest: channelSuggestState{Target: "thread-composer", Query: "g", Start: 0, End: 2, Open: true, Active: 0}}}
		markup := renderNode(t, threadPane(m, h))
		if !strings.Contains(markup, `id="thread-composer-channel-suggest"`) || !strings.Contains(markup, "general") {
			t.Errorf("the thread composer does not draw the open channel list: %s", markup)
		}
		at := strings.Index(markup, `id="thread-composer"`)
		field := markup[strings.LastIndex(markup[:at], "<textarea"):]
		field = field[:strings.Index(field, ">")+1]
		if !strings.Contains(field, `aria-controls="thread-composer-channel-suggest"`) {
			t.Errorf("the reply field does not point at the open list: %s", field)
		}
		closed := renderNode(t, threadPane(m, handlers{}))
		if !strings.Contains(closed, "channel-suggest-slot") || strings.Contains(closed, "thread-composer-channel-suggest") {
			t.Error("the thread composer lost the closed channel slot")
		}
	})

	t.Run("Keys", func(t *testing.T) {
		m := model()
		local := localStore{box: &localUI{channelSuggest: channelSuggestState{Target: "chat-composer", Query: "", Open: true, Active: 0}}}
		if !channelSuggestKey(m, local, "ArrowDown", "chat-composer") || local.get().channelSuggest.Active != 1 {
			t.Fatalf("ArrowDown moved to %d", local.get().channelSuggest.Active)
		}
		if !channelSuggestKey(m, local, "ArrowUp", "chat-composer") || local.get().channelSuggest.Active != 0 {
			t.Fatalf("ArrowUp moved to %d", local.get().channelSuggest.Active)
		}
		if !channelSuggestKey(m, local, "Escape", "chat-composer") || local.get().channelSuggest.Open {
			t.Fatal("Escape did not close the list")
		}
		// Another composer's list is not touched.
		local.box.channelSuggest = channelSuggestState{Target: "thread-composer", Query: "d", Open: true, Active: 0}
		if channelSuggestKey(m, local, "Escape", "chat-composer") || !local.get().channelSuggest.Open {
			t.Error("a key for one composer closed the other's list")
		}
		// Enter with nothing highlighted closes the list and sends on.
		local.box.channelSuggest = channelSuggestState{Target: "chat-composer", Query: "zzz", Open: true, Active: -1}
		if channelSuggestKey(m, local, "Enter", "chat-composer") || local.get().channelSuggest.Open {
			t.Error("Enter with no match did not fall through to send")
		}
	})
}
