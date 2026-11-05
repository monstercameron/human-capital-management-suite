package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATBUG_099: starting a group message needs only two or more
// people — never a channel-style name — and the keyboard path from the list
// to a message to its thread and back is focusable, visible and announced.
//
// Owned scope: the create dialog and the keyboard path live in this package.
// The latest-wins selection race (tools/uxqual/cmd/journeywasm/chat_state.go)
// and the shell rail's parity (internal/humanwork/productui/shell.go) are
// outside it and stay with their owners.
func TestTodo_CHATBUG_099(t *testing.T) {
	people := []mentionCandidate{{ID: "ann", Name: "Ann Lee"}, {ID: "bob", Name: "Bob Ray"}}
	groupWith := func(picked []mentionCandidate) (Model, handlers) {
		m := Model{Locale: "en-US", ShowCreate: true, NewKind: GroupChat, CurrentUser: "walt",
			Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}},
			Callbacks:     walkCallbacks()}
		return m, handlers{local: localUI{picked: picked, createKind: GroupChat}}
	}

	t.Run("GroupNeedsPeople", func(t *testing.T) {
		// The group name is optional and says so; the channel naming rule,
		// placeholder and hint never apply to it.
		m, h := groupWith(people)
		dialog := renderNode(t, createDialog(m, h))
		nameAt := strings.Index(dialog, `id="new-chat-name"`)
		if nameAt < 0 {
			t.Fatal("the group dialog has no name field")
		}
		field := dialog[strings.LastIndex(dialog[:nameAt], "<input"):]
		field = field[:strings.Index(field, ">")+1]
		if strings.Contains(field, "required") {
			t.Errorf("the group name is required: %s", field)
		}
		if strings.Contains(field, "design-reviews") {
			t.Errorf("the group name carries the channel placeholder: %s", field)
		}
		if strings.Contains(dialog, "Lowercase, no spaces") {
			t.Error("the group dialog shows the channel naming hint")
		}
		if !strings.Contains(dialog, "Leave blank to name the group after its people.") {
			t.Errorf("the group name does not say it is optional: %s", dialog)
		}
		if !strings.Contains(dialog, "two or more people") {
			t.Errorf("the group dialog does not say it needs two or more people: %s", dialog)
		}
	})

	t.Run("GroupSubmitGate", func(t *testing.T) {
		// The group starts with two people picked; with fewer the submit
		// stays disabled and nothing is sent.
		for _, tc := range []struct {
			picked   []mentionCandidate
			disabled bool
		}{
			{nil, true},
			{people[:1], true},
			{people, false},
		} {
			m, h := groupWith(tc.picked)
			dialog := renderNode(t, createDialog(m, h))
			submitAt := strings.LastIndex(dialog, `type="submit"`)
			submit := dialog[strings.LastIndex(dialog[:submitAt], "<button"):]
			submit = submit[:strings.Index(submit, ">")+1]
			if strings.Contains(submit, "disabled") != tc.disabled {
				t.Errorf("%d picked: submit disabled state is wrong: %s", len(tc.picked), submit)
			}
		}
	})

	t.Run("GroupNameComposed", func(t *testing.T) {
		// A blank group name is composed from the picked people; a typed
		// name is kept as typed, never channel-normalised.
		if got := createGroupName("", people); got != "Ann Lee, Bob Ray" {
			t.Errorf("a blank group name composes %q", got)
		}
		if got := createGroupName("  Weekend trip ", people); got != "Weekend trip" {
			t.Errorf("a typed group name composes %q", got)
		}
	})

	t.Run("ChannelRulesStayChannelOnly", func(t *testing.T) {
		// Channels still require a name, still validate it, and still print
		// the rule; the group's freedom changes nothing there.
		m := Model{Locale: "en-US", ShowCreate: true, NewKind: PublicChannel, Callbacks: walkCallbacks()}
		dialog := renderNode(t, createDialog(m, handlers{local: localUI{createKind: PublicChannel}}))
		nameAt := strings.Index(dialog, `id="new-chat-name"`)
		field := dialog[strings.LastIndex(dialog[:nameAt], "<input"):]
		field = field[:strings.Index(field, ">")+1]
		if !strings.Contains(field, "required") {
			t.Errorf("the channel name is no longer required: %s", field)
		}
		if !strings.Contains(dialog, "Lowercase, no spaces") {
			t.Error("the channel naming hint is gone")
		}
		if _, ok := createNameForSubmit(localStore{box: &localUI{}}, PublicChannel, "!!!"); ok {
			t.Error("an invalid channel name passes submit validation")
		}
		if name, ok := createNameForSubmit(localStore{box: &localUI{}}, PublicChannel, "Bad Name!"); !ok || name != "bad-name" {
			t.Errorf("a rough channel name submits %q, %v", name, ok)
		}
	})

	t.Run("KeyboardPath", func(t *testing.T) {
		// List row, message, reply count, thread pane, back control: every
		// stop is natively focusable, named, and in tab order; focus is
		// always drawn.
		m := Model{Locale: "en-US", SelectedID: "general", CurrentUser: "walt",
			Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Joined: true, MemberCount: 3}},
			Messages:      []Message{{ID: "p1", AuthorID: "ann", Author: "Ann Lee", Body: "Shall we?", TimeLabel: "09:41", Replies: 2}},
			ShowThread:    true, ThreadParentID: "p1",
			ThreadMessages: []Message{{ID: "r1", Author: "Bob Ray", Body: "Yes."}},
			Callbacks:      walkCallbacks()}
		page := render(t, m)
		stops := []struct {
			name   string
			anchor string
		}{
			{"rail row", `data-action="select" data-id="general"`},
			{"message", `data-message-id="p1"`},
			{"reply count", `data-action="stats"`},
			{"thread pane", `class="chat-side thread-pane"`},
			{"back control", `data-action="close-thread"`},
		}
		previous := -1
		for _, stop := range stops {
			at := strings.Index(page, stop.anchor)
			if at < 0 {
				t.Fatalf("the keyboard path misses its %s", stop.name)
			}
			if at < previous {
				t.Fatalf("the %s is out of tab order", stop.name)
			}
			previous = at
		}
		row := page[strings.LastIndex(page[:strings.Index(page, `data-action="select" data-id="general"`)], "<button"):]
		if strings.Contains(row[:strings.Index(row, ">")+1], "disabled") {
			t.Error("the rail row is not pressable")
		}
		article := page[strings.LastIndex(page[:strings.Index(page, `data-message-id="p1"`)], "<article"):]
		if !strings.Contains(article[:strings.Index(article, ">")+1], `tabindex="0"`) {
			t.Error("the message is not in the tab order")
		}
		if !strings.Contains(page, `aria-label="Close thread"`) {
			t.Error("the back control is not named")
		}
		if !strings.Contains(Stylesheet, `.chat-workspace :focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}`) {
			t.Error("focus is no longer always drawn")
		}
	})

	t.Run("SelectionAnnounced", func(t *testing.T) {
		// A live region names the open conversation, and the open thread, so
		// the keyboard path is announced as well as drawn.
		m := Model{Locale: "en-US", SelectedID: "general", CurrentUser: "walt",
			Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Joined: true}},
			Callbacks:     walkCallbacks()}
		if page := render(t, m); !strings.Contains(page, `id="chat-selection-announcer"`) || !strings.Contains(page, ">general<") {
			t.Errorf("selecting a conversation is not announced: %s", page)
		}
		m.ShowThread, m.ThreadParentID = true, "p1"
		m.Messages = []Message{{ID: "p1", Author: "Ann Lee", Body: "Shall we?"}}
		if page := render(t, m); !strings.Contains(page, "Thread, general") {
			t.Errorf("the open thread is not announced: %s", page)
		}
	})
}
