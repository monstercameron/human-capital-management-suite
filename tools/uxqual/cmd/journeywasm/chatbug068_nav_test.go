package main

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func chatbug068Rooms() []chatui.Conversation {
	return []chatui.Conversation{
		{ID: "ch-general", Name: "general", Kind: chatui.PublicChannel, Joined: true},
		{ID: "ch-random", Name: "random", Kind: chatui.PublicChannel, Joined: true},
		{ID: "dm-policy", Name: "Policy Helper", Kind: chatui.DirectMessage, Joined: true},
	}
}

// Every re-read of the list used to be able to move the tab: a stale address, a
// conversation that dropped out of the list, a first-listed default. Each cause
// is one row.
func TestTodo_CHATBUG_068_ListReadNeverMovesTheOpenConversation(t *testing.T) {
	rooms := chatbug068Rooms()
	cases := []struct {
		name                                   string
		previous, address                      string
		readable, preview, wasListed, complete bool
		wantSelected                           string
		wantUnreadable                         bool
	}{
		{name: "a stream or membership event re-reads the list with the address still on another room", previous: "ch-general", address: "dm-policy", readable: true, wasListed: true, complete: true, wantSelected: "ch-general"},
		{name: "a second tab's navigation reached this tab's address through the shared history", previous: "ch-random", address: "ch-general", readable: true, wasListed: true, complete: true, wantSelected: "ch-random"},
		{name: "a reconnect re-reads an address that names a room the tab already left", previous: "ch-general", address: "ch-random", readable: true, wasListed: true, complete: true, wantSelected: "ch-general"},
		{name: "the open room is a channel being previewed", previous: "ch-news", address: "ch-random", preview: true, wantSelected: "ch-news"},
		{name: "the open room dropped out of a complete list: stay and say so", previous: "ch-news", address: "ch-random", wasListed: true, complete: true, wantSelected: "ch-news", wantUnreadable: true},
		{name: "the open room is missing from a first page only: stay, say nothing", previous: "ch-news", wasListed: true, wantSelected: "ch-news"},
		{name: "a room opened from a search result was never in the list: stay", previous: "ch-news", complete: true, wantSelected: "ch-news"},
		{name: "the first load opens the room the address names", address: "ch-random", complete: true, wantSelected: "ch-random"},
		{name: "the first load with no address leaves the default to the first listed room", complete: true, wantSelected: ""},
	}
	for _, tc := range cases {
		selected, unreadable := chatListingSelection(tc.previous, tc.address, rooms, tc.readable, tc.preview, tc.wasListed, tc.complete)
		if selected != tc.wantSelected || unreadable != tc.wantUnreadable {
			t.Errorf("%s: selected %q unreadable %t, want %q %t", tc.name, selected, unreadable, tc.wantSelected, tc.wantUnreadable)
		}
	}
}

// Another session of the same person saves a draft for the conversation this
// tab has open. The tab keeps its composer and its conversation, tells the
// person, and keeps its own text as the draft; an empty composer takes the
// stored draft quietly.
func TestTodo_CHATBUG_068_DraftFromAnotherSessionNeverReplacesTheComposer(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "northwind", Subject: "avery", Locale: "en-US"}
	open := func(composer string) *chatState {
		state := newChatStateForTest(t)
		state.mutate(func(m *chatui.Model) {
			m.SelectedID, m.Draft = "ch-general", composer
			m.Locale = "en-US"
			m.Conversations = chatbug068Rooms()
		})
		return state
	}
	want := chatui.DraftUpdatedElsewhereText("en-US")

	t.Run("a first read of stored drafts", func(t *testing.T) {
		state := open("typed here")
		state.loadDrafts(map[string]string{"ch-general": "@Po from the phone", "ch-random": "elsewhere"})
		model := state.snapshot()
		if model.SelectedID != "ch-general" || model.Draft != "typed here" || state.draft("ch-general") != "typed here" {
			t.Fatalf("the composer or the conversation moved: selected %q draft %q stored %q", model.SelectedID, model.Draft, state.draft("ch-general"))
		}
		if model.Notice != want {
			t.Fatalf("notice = %q, want %q", model.Notice, want)
		}
		if state.draft("ch-random") != "elsewhere" {
			t.Fatalf("a draft for a room that is not open was not adopted")
		}
	})

	t.Run("a rebase after a revision conflict", func(t *testing.T) {
		state := open("typed here")
		_, _, epoch := state.draftsForWrite()
		drafts, _, current := state.rebaseDrafts(map[string]string{"ch-general": "@Po from the phone"}, cfg, epoch)
		model := state.snapshot()
		if !current || model.SelectedID != "ch-general" || model.Draft != "typed here" || drafts["ch-general"] != "typed here" {
			t.Fatalf("the composer or the conversation moved: current %t selected %q draft %q stored %+v", current, model.SelectedID, model.Draft, drafts)
		}
		if model.Notice != want {
			t.Fatalf("notice = %q, want %q", model.Notice, want)
		}
	})

	t.Run("an empty composer takes the stored draft without a notice", func(t *testing.T) {
		state := open("")
		state.loadDrafts(map[string]string{"ch-general": "from the phone"})
		if model := state.snapshot(); model.Draft != "from the phone" || model.Notice != "" {
			t.Fatalf("draft %q notice %q", model.Draft, model.Notice)
		}
	})

	t.Run("the same text on both sides is no news", func(t *testing.T) {
		state := open("same")
		state.loadDrafts(map[string]string{"ch-general": "same"})
		if model := state.snapshot(); model.Draft != "same" || model.Notice != "" {
			t.Fatalf("draft %q notice %q", model.Draft, model.Notice)
		}
	})
}

// Two sessions of one person, each with its own open conversation: what one
// does reaches the other only as stored drafts, and neither is moved.
func TestTodo_CHATBUG_068_TwoSessionsOfOnePersonDoNotMoveEachOther(t *testing.T) {
	cfg := journeyclient.Config{Tenant: "northwind", Subject: "avery", Locale: "en-US"}
	a, b := newChatStateForTest(t), newChatStateForTest(t)
	a.mutate(func(m *chatui.Model) { m.SelectedID, m.Conversations = "ch-general", chatbug068Rooms() })
	b.mutate(func(m *chatui.Model) { m.SelectedID, m.Conversations = "dm-policy", chatbug068Rooms() })

	// Session B types in its own conversation and opens another one; both reach
	// the server as the person's stored drafts, which session A reads on its next
	// conflict.
	b.setDraft("dm-policy", "@Po")
	b.selectChatConversation("ch-random")
	b.setDraft("ch-random", "plans for tonight")
	stored := b.allDrafts()

	a.setDraft("ch-general", "typing in general")
	_, _, epoch := a.draftsForWrite()
	a.rebaseDrafts(stored, cfg, epoch)
	a.loadDrafts(stored)
	if model := a.snapshot(); model.SelectedID != "ch-general" || model.Draft != "typing in general" {
		t.Fatalf("session B moved session A: selected %q draft %q", model.SelectedID, model.Draft)
	}
	if a.draft("dm-policy") != "@Po" {
		t.Fatalf("session A does not offer the other conversation's stored draft: %q", a.draft("dm-policy"))
	}
	if model := b.snapshot(); model.SelectedID != "ch-random" || model.Draft != "plans for tonight" {
		t.Fatalf("session A moved session B: selected %q draft %q", model.SelectedID, model.Draft)
	}
}

// The whole path of a list read, as the page takes it: another session's
// stored draft arrives with a list read whose address still names a different
// room, and the room open in this tab, its draft and its place do not change.
func TestTodo_CHATBUG_068(t *testing.T) {
	state := newChatStateForTest(t)
	state.mutate(func(m *chatui.Model) {
		m.SelectedID, m.Draft, m.Locale = "ch-general", "typing in general", "en-US"
		m.Conversations = chatbug068Rooms()
	})
	state.setDraft("ch-general", "typing in general")
	generation := state.currentGeneration()

	loaded := state.snapshot()
	selected, unreadable := chatListingSelection(loaded.SelectedID, "dm-policy", chatbug068Rooms(), true, false, true, true)
	loaded.SelectedID = selected
	if !state.adoptLoadedChatProjection(generation, loaded, chatCursor{}, false) {
		t.Fatal("the list read was refused")
	}
	model := state.snapshot()
	if unreadable || model.SelectedID != "ch-general" || model.Draft != "typing in general" {
		t.Fatalf("a list read moved the tab: selected %q draft %q unreadable %t", model.SelectedID, model.Draft, unreadable)
	}

	// The open room leaves the list for good: it stays on screen, and the
	// person is told in place.
	loaded = state.snapshot()
	loaded.Conversations = chatbug068Rooms()[1:]
	selected, unreadable = chatListingSelection("ch-general", "", loaded.Conversations, false, false, true, true)
	if selected != "ch-general" || !unreadable {
		t.Fatalf("a conversation that stopped being readable was replaced by %q (unreadable %t)", selected, unreadable)
	}
}

// The page's callback for a picked mention is the local re-render: the menu
// stores the choice and writes the draft (neither schedules a render), then
// calls it, and refreshChatRoute is what runs chatRerender. The behavioural
// twin of this check, which calls the callback and counts renders, is
// chatbug068_pick_wasm_test.go.
func TestTodo_CHATBUG_068_MentionPickIsWiredToTheLocalRender(t *testing.T) {
	callbacks := chatperfBody(t, "chat_wasm.go", "func chatCallbacks(")
	if !strings.Contains(callbacks, "MentionPicked: refreshChatRoute,") {
		t.Fatal("chatCallbacks does not give the page a MentionPicked callback that re-renders")
	}
	refresh := chatperfBody(t, "chat_wasm.go", "func refreshChatRoute(")
	if !strings.Contains(refresh, "chatRerender()") {
		t.Fatal("refreshChatRoute no longer re-renders the open page")
	}
}
