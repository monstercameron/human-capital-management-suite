//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestPushChatHistoryForSelectionUpdatesAddressBar is the NAV live finding's
// regression test: creating a channel, creating a DM/group, and self-joining
// from Browse all select the new room by calling openChatConversation
// directly, which -- deliberately, since chat_history_wasm.go's popstate
// restore calls it too -- never touches history itself. Each of those three
// success paths now calls pushChatHistoryForSelection first, exactly the way
// SelectConversation always has; this proves that shared call does what a
// normal rail click's history push does, so a reload or Back after any of
// the three lands on the new room instead of the one open before it.
func TestPushChatHistoryForSelectionUpdatesAddressBar(t *testing.T) {
	browser := installWASMHistory(t, "/workspace/app/chat?locale=en-US")
	previousProductHistory := productHistory
	previousRefresh := productHistoryControlsRefresh
	productHistory = &browserProductHistoryController{id: "0123456789abcdef0123456789abcdef"}
	productHistoryControlsRefresh = func() {}
	t.Cleanup(func() {
		productHistory = previousProductHistory
		productHistoryControlsRefresh = previousRefresh
		chatHistory.reset()
	})
	productState := js.Global().Get("Object").New()
	productState.Set(productHistoryIDField, productHistory.id)
	productState.Set(productHistoryIndexField, 0)
	if !browserHistoryReplaceState(browser.history, productState, browserLocationHref()) {
		t.Fatal("could not seed app history state")
	}
	chatHistory.seed(chatui.Model{CurrentTenantID: "tenant-one", CurrentUser: "subject-one", SelectedID: "room-old"})
	if !chatHistory.seeded {
		t.Fatal("chat history did not seed the initial room")
	}

	oldModel := chatBrowser.snapshot()
	t.Cleanup(func() { chatBrowser.mutate(func(m *chatui.Model) { *m = oldModel }) })
	chatBrowser.mutate(func(m *chatui.Model) {
		m.CurrentTenantID, m.CurrentUser = "tenant-one", "subject-one"
		m.SelectedID = "room-old"
		// The rail's URL kept the previous room's #channel= fragment even
		// with a thread open and a search in progress -- the push must
		// leave both behind, the same as an ordinary SelectConversation.
		m.Search = "leftover search"
		m.ShowThread, m.ThreadParentID = true, "post-1"
	})

	// This is exactly what CreateConversation's, joinChatConversation's and
	// openChatPersonDM's success paths call before opening the new room.
	pushChatHistoryForSelection("room-new")

	if got := browser.location.Get("hash").String(); got != "channel=room-new" {
		t.Fatalf("address bar after selecting the new room = %q, want channel=room-new", got)
	}
	if got := len(browser.entries); got != 2 {
		t.Fatalf("history entries after the push = %d, want 2 (seed + push)", got)
	}
	state, ok := browserHistoryState(browser.history)
	if !ok {
		t.Fatal("no history state after the push")
	}
	nav, valid := readChatNavigationState(state)
	if !valid || nav.ConversationID != "room-new" {
		t.Fatalf("pushed navigation state = %#v valid=%v, want conversation room-new", nav, valid)
	}
	if nav.ShowThread || nav.ThreadParentID != "" {
		t.Fatal("selecting the new room did not close the thread that was open in the old one")
	}
}
