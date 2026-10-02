package chatui

import (
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

// Create conversation explains each kind by its use, so a person can tell a
// private channel from a group message before choosing.
func TestTodo_CHATUX_015(t *testing.T) {
	m := Model{Locale: "en-US", Callbacks: Callbacks{CreateConversation: func(ConversationKind, string, []string) {}}}
	got := renderNode(t, createDialog(m, handlers{}))
	for _, want := range []string{"Private channel", "A lasting topic for invited people.", "Group message", "A quick conversation with a few people.", "A topic or team that anyone in the company can find and join."} {
		if !strings.Contains(got, want) {
			t.Errorf("the create dialog misses %q: %s", want, got)
		}
	}
	if strings.Contains(got, "Private group") || strings.Contains(got, "Only people you invite can see it.") {
		t.Errorf("the create dialog still uses the old kind words: %s", got)
	}
	// The section controls have names and tooltips. The hover-only arrows are
	// gone (CHATSIDE-001): the heading carries the three dots, named and with a
	// tooltip, and the moves are named items in its menu.
	rail := renderNode(t, railSection(chatux020Model(), SidebarSection{ID: "channels", Name: "Channels", Chats: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel}}}, nil))
	for _, want := range []string{`aria-label="Options for section Channels"`, `title="Options for section Channels"`, `aria-haspopup="menu"`} {
		if !strings.Contains(rail, want) {
			t.Errorf("the section menu button misses %q: %s", want, rail)
		}
	}
	if strings.Contains(rail, "section-order") {
		t.Errorf("the section heading still carries the hover arrows: %s", rail)
	}
	menuModel := chatux020Model()
	menuModel.RailMenuID = "section:direct"
	menu := renderNode(t, chatux020RailMenu(menuModel, handlers{}))
	for _, want := range []string{`data-action="section-up" data-id="direct"`, ">Move section up<", `data-action="section-down" data-id="direct"`, ">Move section down<"} {
		if !strings.Contains(menu, want) {
			t.Errorf("the section menu misses %q: %s", want, menu)
		}
	}
}

// Browse channels: each channel's purpose, channels the person has not joined
// first with Join on the row, and the channel's status where it has one.
func TestTodo_CHATUX_015_BrowseChannels(t *testing.T) {
	now := time.Now()
	m := Model{Locale: "en-US", ShowBrowse: true,
		Browse: []Conversation{
			{ID: "design", Name: "design", Kind: PublicChannel, Topic: "Design reviews and critique", MemberCount: 12, LastActivity: now},
			{ID: "announce", Name: "announcements", Kind: PublicChannel, Topic: "Company news", MemberCount: 40, LastActivity: now},
		},
		Conversations:   []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Topic: "Everything else", Joined: true, MemberCount: 18}},
		ChannelStatuses: map[string]ChannelStatusView{"announce": {Status: chat.ChannelStatus{TenantID: "t", ConversationID: "announce", Status: chatpolicy.StatusAnnouncements, Revision: 1}}},
		Callbacks:       Callbacks{JoinConversation: func(string) {}, SelectConversation: func(string) {}, CloseBrowse: func() {}, OpenCreate: func() {}, FilterBrowse: func(string) {}},
	}
	got := renderNode(t, browseDialog(m, handlers{}))
	for _, want := range []string{"Design reviews and critique", "Company news", "Everything else", `data-action="join" data-id="design"`, "Announcements only"} {
		if !strings.Contains(got, want) {
			t.Errorf("Browse channels misses %q: %s", want, got)
		}
	}
	// Joined channels come after the ones that can be joined.
	if strings.Index(got, `data-id="general"`) < strings.Index(got, `data-id="announce"`) || strings.Index(got, `data-id="general"`) < strings.Index(got, `data-id="design"`) {
		t.Errorf("a joined channel is listed before the ones to join: %s", got)
	}
	// An Open channel shows no badge (it is the ordinary case).
	if strings.Count(got, "chatstate-badge") != 1 {
		t.Errorf("%d status badges, want only the announcements one", strings.Count(got, "chatstate-badge"))
	}
}

// Closing a search clears the field.
func TestTodo_CHATUX_015_SearchClose(t *testing.T) {
	var queries []string
	m := Model{Search: "payroll", Callbacks: Callbacks{Search: func(q string) { queries = append(queries, q) }}}
	chatux015CloseSearch(m)
	if len(queries) != 1 || queries[0] != "" {
		t.Fatalf("closing the search sent %q, want one empty query", queries)
	}
	// Nothing typed: nothing to clear and no call.
	queries = nil
	chatux015CloseSearch(Model{Callbacks: m.Callbacks})
	chatux015CloseSearch(Model{Search: "x"})
	if len(queries) != 0 {
		t.Fatalf("an empty or unwired search was cleared: %q", queries)
	}
}
