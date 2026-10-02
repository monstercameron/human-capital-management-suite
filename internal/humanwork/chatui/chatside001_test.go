package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
)

func sideRoom(id string, kind ConversationKind, unread int, starred bool) Conversation {
	return Conversation{ID: id, Name: id, Kind: kind, Joined: true, Unread: unread, Starred: starred}
}

// TestTodo_CHATSIDE_001: how the sidebar arranges a person's own sections:
// Favorites first and only while it holds something, a favorite listed in
// Favorites and nowhere else, sections otherwise in the person's order, an
// archived channel left to the Archived list, and the rules for a section name.
func TestTodo_CHATSIDE_001(t *testing.T) {
	general, sales, dm := sideRoom("general", PublicChannel, 0, true), sideRoom("sales", PublicChannel, 2, false), sideRoom("friend", DirectMessage, 0, false)
	m := Model{Locale: "en-US", Conversations: []Conversation{general, sales, dm},
		Sections: []SidebarSection{
			{ID: "custom-1", Name: "Projects", Chats: []Conversation{sales}},
			{ID: "channels", Name: "Channels", Chats: []Conversation{general}},
			{ID: "direct", Name: "Direct messages", Chats: []Conversation{dm}},
		}}
	got := chatside001Sections(m, m.Sections)
	var ids []string
	for _, section := range got {
		ids = append(ids, section.ID)
	}
	if strings.Join(ids, ",") != "favorites,custom-1,channels,direct" {
		t.Fatalf("section order = %v", ids)
	}
	if len(got[0].Chats) != 1 || got[0].Chats[0].ID != "general" || len(got[2].Chats) != 0 {
		t.Errorf("a favorite must be listed in Favorites only: favorites=%v channels=%v", got[0].Chats, got[2].Chats)
	}
	if got[0].Name != "Favorites" {
		t.Errorf("Favorites is called %q", got[0].Name)
	}
	m.Conversations[0].Starred = false
	if again := chatside001Sections(m, m.Sections); again[0].ID == "favorites" || len(again) != 3 {
		t.Errorf("Favorites is shown while it holds nothing: %v", again)
	}
	m.Conversations[0].Starred = true
	m.FavoritesCollapsed = true
	if folded := chatside001Sections(m, m.Sections); !folded[0].Collapsed {
		t.Error("the person's choice to fold Favorites is lost")
	}

	// Names: one to forty characters, not Favorites, not one the person has, and
	// at most twenty sections of their own.
	for name, want := range map[string]string{
		"":                      "Give the section a name.",
		"   ":                   "Give the section a name.",
		strings.Repeat("x", 41): "Use 40 characters or fewer.",
		"favorites":             "Favorites is already a section.",
		" projects ":            "You already have a section with that name.",
		strings.Repeat("é", 40): "",
		"Reading":               "",
		"Channels":              "",
	} {
		if msg := chatside001NameError(m, name, ""); msg != want {
			t.Errorf("name %q: %q, want %q", name, msg, want)
		}
	}
	if msg := chatside001NameError(m, "Projects", "custom-1"); msg != "" {
		t.Errorf("a section may keep its own name: %q", msg)
	}
	full := m
	for i := 0; i < chatside001CustomMax; i++ {
		full.Sections = append(full.Sections, SidebarSection{ID: "custom-" + strings.Repeat("n", i+2), Name: "S" + strings.Repeat("n", i)})
	}
	if msg := chatside001NameError(full, "One more", ""); msg != "You can have up to 20 sections of your own." {
		t.Errorf("the twenty-first section: %q", msg)
	}

	// Moving by one place follows the person's order, built-in sections included.
	if Chatside001Neighbor(m.Sections, "custom-1", -1) != -1 || Chatside001Neighbor(m.Sections, "custom-1", 1) != 1 || Chatside001Neighbor(m.Sections, "direct", 1) != -1 || Chatside001Neighbor(m.Sections, "nowhere", 1) != -1 {
		t.Error("section neighbours are wrong")
	}
	// A folded heading's total counts the section's whole content.
	if total, mention := chatside001UnreadTotal(SidebarSection{Chats: []Conversation{{Unread: 3}, {Unread: 1, Mentions: 2}, {}}}); total != 5 || !mention {
		t.Errorf("unread total = %d, mention %v", total, mention)
	}
	// An archived channel stays under Archived: the sections drop it before the arrangement.
	if archived := chatside001Starred(Model{Conversations: []Conversation{general}}); !archived["general"] {
		t.Error("the starred set misses a starred conversation")
	}
	if chatside001KindOf(dm) != "direct" || chatside001Accepts("channels") != "channel" || chatside001Accepts("custom-1") != "any" {
		t.Error("the drag kinds are wrong")
	}
}

// One sentence a person can predict: Favorites first, then their sections in the
// order they arranged (their own and the built-in ones alike, never regrouped),
// and Archived always last. A saved order is kept exactly, and the heading
// carries one three-dots menu and no hover arrows.
func TestTodo_CHATSIDE_001_OrderRule(t *testing.T) {
	general, ops, dm := sideRoom("general", PublicChannel, 0, true), sideRoom("ops", PublicChannel, 0, false), sideRoom("friend", DirectMessage, 0, false)
	for _, order := range [][]string{
		{"direct", "custom-a", "channels", "custom-b"},
		{"custom-a", "direct", "custom-b", "channels"},
		{"channels", "custom-a", "custom-b", "direct"},
	} {
		byID := map[string]SidebarSection{
			"channels": {ID: "channels", Name: "Channels", Chats: []Conversation{general}},
			"direct":   {ID: "direct", Name: "Direct messages", Chats: []Conversation{dm}},
			"custom-a": {ID: "custom-a", Name: "Alpha", Chats: []Conversation{ops}},
			"custom-b": {ID: "custom-b", Name: "Beta"},
		}
		var sections []SidebarSection
		for _, id := range order {
			sections = append(sections, byID[id])
		}
		m := Model{State: StateReady, Locale: "en-US", Conversations: []Conversation{general, ops, dm}, Sections: sections,
			ChatFeatures:    &ChatFeatures{Status: true},
			ChannelStatuses: map[string]ChannelStatusView{"old": {Status: chat.ChannelStatus{TenantID: "t", ConversationID: "old", Name: "old", Status: chatpolicy.StatusArchived, Revision: 1}}},
			Callbacks:       Callbacks{ToggleSection: func(string) {}, OpenRailMenu: func(string) {}}}
		markup := renderNode(t, rail(m, handlers{}))
		want := append([]string{"favorites"}, order...)
		last := -1
		for _, id := range want {
			at := strings.Index(markup, `data-section-id="`+id+`"`)
			if at < 0 || at < last {
				t.Fatalf("order %v: section %s is missing or out of order (at %d, after %d)", order, id, at, last)
			}
			last = at
		}
		if archived := strings.Index(markup, "chatstate-archived"); archived < last {
			t.Errorf("order %v: Archived (%d) is not after the last section (%d)", order, archived, last)
		}
	}
	// The heading: one named menu button, no hover arrows, no cross.
	got := renderNode(t, railSection(Model{Locale: "en-US", Callbacks: Callbacks{ToggleSection: func(string) {}, OpenRailMenu: func(string) {}, ReorderSection: func(string, int) {}, RemoveSection: func(string) {}}}, SidebarSection{ID: "custom-a", Name: "Alpha"}, nil))
	if strings.Count(got, `data-action="rail-menu"`) != 1 || strings.Contains(got, "section-order") || strings.Contains(got, `data-action="section-`) {
		t.Errorf("the heading has more than its three-dots button: %s", got)
	}
}

// The star in the header says what pressing it does and whether it is on, in
// every language: a name that changes with the state, a pressed state and a
// tooltip with the same words.
func TestTodo_CHATSIDE_001_Star(t *testing.T) {
	for _, tc := range []struct {
		locale, add, remove string
	}{
		{"en-US", "Add to favorites", "Remove from favorites"},
		{"de-DE", "Zu Favoriten hinzufügen", "Aus Favoriten entfernen"},
		{"ar", "إضافة إلى المفضلة", "إزالة من المفضلة"},
	} {
		for _, starred := range []bool{false, true} {
			room := Conversation{ID: "general", Name: "general", Kind: PublicChannel, Joined: true, Starred: starred}
			m := Model{Locale: tc.locale, Conversations: []Conversation{room}, Callbacks: Callbacks{SetFavorite: func(string, bool) {}}}
			star := renderNode(t, ui.Fragment(chatside001HeaderStar(m, room, nil)...))
			label, pressed := tc.add, "false"
			if starred {
				label, pressed = tc.remove, "true"
			}
			for _, want := range []string{`aria-label="` + label + `"`, `title="` + label + `"`, `aria-pressed="` + pressed + `"`, `data-action="side-fav"`} {
				if !strings.Contains(star, want) {
					t.Errorf("%s starred=%v: the star lacks %s: %s", tc.locale, starred, want, star)
				}
			}
			other := tc.remove
			if starred {
				other = tc.add
			}
			if strings.Contains(star, other) {
				t.Errorf("%s starred=%v: the star also carries the other state's name %q", tc.locale, starred, other)
			}
		}
	}
}

// On touch a long press on a row opens the row menu; a mouse or a pen drags the
// row onto a section instead, and a finger never drags (it scrolls the list).
func TestTodo_CHATSIDE_001_Touch(t *testing.T) {
	for pointer, want := range map[string][2]bool{"touch": {false, true}, "mouse": {true, false}, "pen": {true, false}, "": {true, false}} {
		if drag, long := chatside001Gesture(pointer); drag != want[0] || long != want[1] {
			t.Errorf("pointer %q: drag=%v longPress=%v, want %v", pointer, drag, long, want)
		}
	}
	if chatside001LongPressMs < 400 || chatside001LongPressMs > 800 {
		t.Errorf("a long press of %d ms is neither a tap nor a hold", chatside001LongPressMs)
	}
	// What the browser half looks for: the row carries its conversation id and
	// kind, and has its three-dots button, which the long press opens the menu of.
	m := chatux020Model()
	row := renderNode(t, railRow(m, m.Conversations[1]))
	for _, want := range []string{`data-conversation-id="quiet"`, `data-side-kind="channel"`, `data-action="rail-menu" data-id="quiet"`} {
		if !strings.Contains(row, want) {
			t.Errorf("a sidebar row lacks %s: %s", want, row)
		}
	}
	// A press that is held must not select text or raise the system menu.
	if !strings.Contains(ChatSide001Styles, "@media(hover:none){.chat-rail-row{-webkit-touch-callout:none") {
		t.Error("a touch row can still select text or raise the system menu on a long press")
	}
}
