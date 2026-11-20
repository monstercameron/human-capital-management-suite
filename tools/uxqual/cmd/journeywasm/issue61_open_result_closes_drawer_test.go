package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestIssue61_OpeningAResultClosesThePhoneDrawer: on a phone the open
// Conversations drawer hides the conversation pane, so a result that opened its
// conversation behind the drawer left the person on the list (found in the
// browser at 320 and 390 px). Opening a result must close the drawer.
func TestIssue61_OpeningAResultClosesThePhoneDrawer(t *testing.T) {
	m := chatui.Model{SidebarOpen: true}
	chatsearchModelOpened(&m, chatui.ChatSearchView{Query: "meeting"})
	if m.SidebarOpen {
		t.Fatal("the Conversations drawer stays open after a result is opened, hiding the conversation")
	}
	if !m.SearchOpened || m.Search != "meeting" {
		t.Fatalf("opening a result must keep the query and the return bar, got opened=%v search=%q", m.SearchOpened, m.Search)
	}
}
