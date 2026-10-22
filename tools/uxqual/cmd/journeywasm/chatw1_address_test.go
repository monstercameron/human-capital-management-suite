package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatw1Tab is the part of a tab that the address of Moderation and Saved
// depends on: the history the browser keeps, the page on screen, and the
// fragment the page last wrote (chatPageRoute.applied). Each method is one of
// the ways the address of those two pages is reached, and drives the same
// functions the browser build does.
type chatw1Tab struct {
	state   *chatState
	history []string
	at      int
	onShow  string
	applied string
}

func newChatw1Tab(t *testing.T, state *chatState) *chatw1Tab {
	t.Helper()
	return &chatw1Tab{state: state, history: []string{""}}
}

func (tab *chatw1Tab) hash() string { return tab.history[tab.at] }

// restore is chatPageRestore: the screen moves to the page the address names and
// remembers the address as its own, so nothing is written back.
func (tab *chatw1Tab) restore() {
	kind, query := parseChatPageFragment(tab.hash())
	tab.onShow, tab.applied = kind, chatPageFragment(kind, query)
	if id := chatw1ChannelOf(tab.hash()); id == "" {
		tab.state.claimChannelFragment("")
	} else {
		tab.state.claimChannelFragment("northwind\x00avery\x00" + tab.hash())
	}
}

// load is a direct load or a reload of the address.
func (tab *chatw1Tab) load(hash string) {
	tab.history[tab.at] = hash
	tab.restore()
}

// typed is an address typed into the open tab: a new history entry, then read.
func (tab *chatw1Tab) typed(hash string) {
	tab.history = append(tab.history[:tab.at+1], hash)
	tab.at++
	tab.restore()
}

// click is a click on the sidebar's Moderation or Saved row: the page opens and
// chatPageSync writes the address it names, as a new history entry.
func (tab *chatw1Tab) click(kind string) {
	tab.onShow = kind
	shown, query := chatPageShowing(kind == chatui.ChatPageModeration, kind == chatui.ChatPageSaved, "", false)
	if fragment := chatPageFragment(shown, query); fragment != tab.applied {
		tab.history = append(tab.history[:tab.at+1], fragment)
		tab.at++
		tab.applied = fragment
		tab.state.claimChannelFragment("")
	}
}

// back and forward are the browser's buttons: the entry's address is read.
func (tab *chatw1Tab) back()    { tab.at--; tab.restore() }
func (tab *chatw1Tab) forward() { tab.at++; tab.restore() }

func chatw1ChannelOf(hash string) string {
	if len(hash) > len("#channel=") && hash[:len("#channel=")] == "#channel=" {
		return hash[len("#channel="):]
	}
	return ""
}

// TestTodo_CHATBUG_068_ModerationAndSavedAddresses walks the five ways to reach
// the Moderation page and the Saved panel by address: a direct load, a click on
// the sidebar row, an address typed into an open tab, Back and Forward, and a
// reload while the page is open. The page must be on screen after each, the
// address it carries must be the page's own, and a conversation address typed
// afterwards must still be read.
func TestTodo_CHATBUG_068_ModerationAndSavedAddresses(t *testing.T) {
	for _, page := range []struct{ kind, hash string }{
		{chatui.ChatPageModeration, "#moderation"},
		{chatui.ChatPageSaved, "#saved"},
	} {
		t.Run(page.kind, func(t *testing.T) {
			state := newChatStateForTest(t)

			// 1. A direct load opens the page and writes nothing more.
			tab := newChatw1Tab(t, state)
			tab.load(page.hash)
			if tab.onShow != page.kind || tab.applied != page.hash || len(tab.history) != 1 {
				t.Fatalf("direct load: showing %q applied %q history %v", tab.onShow, tab.applied, tab.history)
			}

			// 2. A click on the sidebar row, from a conversation, adds one entry.
			tab = newChatw1Tab(t, state)
			tab.load("#channel=general")
			tab.click(page.kind)
			if tab.onShow != page.kind || tab.hash() != page.hash || len(tab.history) != 2 {
				t.Fatalf("row click: showing %q address %q history %v", tab.onShow, tab.hash(), tab.history)
			}
			// 4. Back returns to the conversation and closes the page; Forward
			// brings it back.
			tab.back()
			if tab.onShow != "" || tab.hash() != "#channel=general" {
				t.Fatalf("back: showing %q address %q", tab.onShow, tab.hash())
			}
			tab.forward()
			if tab.onShow != page.kind || tab.hash() != page.hash {
				t.Fatalf("forward: showing %q address %q", tab.onShow, tab.hash())
			}
			// 5. A reload with the page open reads the same address.
			tab.load(tab.hash())
			if tab.onShow != page.kind || tab.applied != page.hash {
				t.Fatalf("reload: showing %q applied %q", tab.onShow, tab.applied)
			}

			// 3. The address typed into a tab that has a conversation open.
			tab = newChatw1Tab(t, state)
			tab.load("#channel=random")
			tab.typed(page.hash)
			if tab.onShow != page.kind || tab.applied != page.hash {
				t.Fatalf("typed: showing %q applied %q", tab.onShow, tab.applied)
			}
			// The conversation address typed next is news, not a repeat.
			if !state.claimChannelFragment("northwind\x00avery\x00#channel=random") {
				t.Fatal("a conversation address typed after the page was ignored")
			}
			tab.typed("#channel=random")
			if tab.onShow != "" || tab.applied != "" {
				t.Fatalf("typing a conversation left the page on screen: %q %q", tab.onShow, tab.applied)
			}
		})
	}
}
