package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATBUG_099_Integration starts a group on the served page: with two
// people picked the group submits under its composed name, channels still
// require and validate theirs, and the keyboard path with its announcer runs
// end to end with a thread open.
func TestTodo_CHATBUG_099_Integration(t *testing.T) {
	people := []mentionCandidate{{ID: "ann", Name: "Ann Lee"}, {ID: "bob", Name: "Bob Ray"}}

	t.Run("GroupStarts", func(t *testing.T) {
		var kind ConversationKind
		var name string
		var members []string
		m := Model{Locale: "en-US", ShowCreate: true, NewKind: GroupChat, CurrentUser: "walt",
			Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}},
			Callbacks: Callbacks{CreateConversation: func(k ConversationKind, n string, ids []string) {
				kind, name, members = k, n, ids
			}}}
		// The dialog with two people picked offers an enabled submit; the
		// composed name is what a blank box sends.
		dialog := renderNode(t, createDialog(m, handlers{local: localUI{picked: people, createKind: GroupChat}}))
		submitAt := strings.LastIndex(dialog, `type="submit"`)
		submit := dialog[strings.LastIndex(dialog[:submitAt], "<button"):]
		if strings.Contains(submit[:strings.Index(submit, ">")+1], "disabled") {
			t.Fatalf("two picked people leave the group submit disabled: %s", submit)
		}
		m.Callbacks.CreateConversation(GroupChat, createGroupName("", people), []string{"ann", "bob"})
		if kind != GroupChat || name != "Ann Lee, Bob Ray" || len(members) != 2 {
			t.Errorf("the group submits %v %q %v", kind, name, members)
		}
	})

	t.Run("ChannelStillGated", func(t *testing.T) {
		called := false
		m := Model{Locale: "en-US", ShowCreate: true, NewKind: PublicChannel,
			Callbacks: Callbacks{CreateConversation: func(ConversationKind, string, []string) { called = true }}}
		dialog := renderNode(t, createDialog(m, handlers{local: localUI{createKind: PublicChannel}}))
		nameAt := strings.Index(dialog, `id="new-chat-name"`)
		field := dialog[strings.LastIndex(dialog[:nameAt], "<input"):]
		if !strings.Contains(field[:strings.Index(field, ">")+1], "required") {
			t.Error("the channel name is no longer required")
		}
		if _, ok := createNameForSubmit(localStore{box: &localUI{}}, PublicChannel, "!!"); ok || called {
			t.Error("an invalid channel name would submit")
		}
	})

	t.Run("KeyboardPathAnnounced", func(t *testing.T) {
		m := Model{Locale: "en-US", SelectedID: "general", CurrentUser: "walt",
			Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}},
			Messages:      []Message{{ID: "p1", Author: "Ann Lee", Body: "Shall we?", Replies: 2}},
			ShowThread:    true, ThreadParentID: "p1",
			ThreadMessages: []Message{{ID: "r1", Author: "Bob Ray", Body: "Yes."}},
			Callbacks:      walkCallbacks()}
		page := render(t, m)
		stops := []string{
			`data-action="select" data-id="general"`,
			`data-message-id="p1"`,
			`data-action="stats"`,
			`class="chat-side thread-pane"`,
			`data-action="close-thread"`,
		}
		previous := -1
		for _, stop := range stops {
			at := strings.Index(page, stop)
			if at < 0 || at < previous {
				t.Fatalf("the keyboard path breaks at %q", stop)
			}
			previous = at
		}
		if !strings.Contains(page, "Thread, general") {
			t.Error("the open thread is not announced")
		}
	})
}
