package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATSIDE_001_Browser renders the sidebar with the product's own
// catalog in each language: Favorites first, the person's own sections in their
// order and then the built-in ones, a folded section that still shows the open
// conversation and its unread total, the row menu's favorite and move items,
// the section menu and the star in the header.
func TestTodo_CHATSIDE_001_Browser(t *testing.T) {
	type words struct{ favorites, add, remove, moveFav, moveNew, rename, del, unread string }
	for locale, w := range map[string]words{
		"en-US": {"Favorites", "Add to favorites", "Remove from favorites", "Move to Favorites", "Move to a new section…", "Rename section", "Delete section", "4 unread in Projects"},
		"de-DE": {"Favoriten", "Zu Favoriten hinzufügen", "Aus Favoriten entfernen", "Nach Favoriten verschieben", "In einen neuen Bereich verschieben…", "Bereich umbenennen", "Bereich löschen", "4 ungelesen in Projects"},
		"ar":    {"المفضلة", "إضافة إلى المفضلة", "إزالة من المفضلة", "نقل إلى المفضلة", "نقل إلى قسم جديد…", "إعادة تسمية القسم", "حذف القسم", "٤ غير مقروءة في Projects"},
	} {
		room := func(id string, unread int, starred bool) chatui.Conversation {
			return chatui.Conversation{ID: id, Name: id, Kind: chatui.PublicChannel, Joined: true, Unread: unread, Starred: starred}
		}
		build := func(menu string) func(*chatui.Model) {
			return func(m *chatui.Model) {
				general, ops, idle, news, dm := room("general", 0, false), room("ops", 4, false), room("idle", 0, false), room("news", 0, true), room("friend", 0, false)
				dm.Kind = chatui.DirectMessage
				m.Conversations = []chatui.Conversation{general, ops, idle, news, dm}
				m.SelectedID = "general"
				m.Sections = []chatui.SidebarSection{
					{ID: "custom-1", Name: "Projects", Collapsed: true, Chats: []chatui.Conversation{general, ops}},
					{ID: "custom-2", Name: "Reading"},
					{ID: "channels", Name: "Channels", Chats: []chatui.Conversation{idle, news}},
					{ID: "direct", Name: "Direct messages", Chats: []chatui.Conversation{dm}},
				}
				m.RailMenuID = menu
				m.Callbacks.SetFavorite, m.Callbacks.MoveConversationSection, m.Callbacks.CreateSectionFor = func(string, bool) {}, func(string, string) {}, func(string, string) {}
				m.Callbacks.RenameSection, m.Callbacks.RemoveSection, m.Callbacks.ReorderSection, m.Callbacks.ToggleSection = func(string, string) {}, func(string) {}, func(string, int) {}, func(string) {}
			}
		}
		page := lane3Page(t, locale, build(""))
		lane3NoLeaks(t, locale+" sidebar", page)
		// The order: Favorites, the person's sections, then Channels and Direct messages.
		last := -1
		for _, id := range []string{"favorites", "custom-1", "custom-2", "channels", "direct"} {
			at := strings.Index(page, `data-section-id="`+id+`"`)
			if at < 0 || at < last {
				t.Fatalf("%s: section %s is missing or out of order (at %d, after %d)", locale, id, at, last)
			}
			last = at
		}
		favorites := page[strings.Index(page, `data-section-id="favorites"`):strings.Index(page, `data-section-id="custom-1"`)]
		if !strings.Contains(favorites, w.favorites) || !strings.Contains(favorites, `data-id="news"`) {
			t.Errorf("%s: Favorites does not hold the starred conversation: %s", locale, favorites)
		}
		if channels := page[strings.Index(page, `data-section-id="channels"`):]; strings.Contains(channels[:strings.Index(channels, `data-section-id="direct"`)], `data-id="news"`) {
			t.Errorf("%s: a favorite is also listed in its own section", locale)
		}
		// The folded section keeps the open conversation and the unread one, and its total.
		projects := page[strings.Index(page, `data-section-id="custom-1"`):strings.Index(page, `data-section-id="custom-2"`)]
		for _, want := range []string{`data-id="general"`, `data-id="ops"`, w.unread} {
			if !strings.Contains(projects, want) {
				t.Errorf("%s: the folded section lacks %q", locale, want)
			}
		}
		if strings.Contains(projects, `data-id="idle"`) {
			t.Errorf("%s: the folded section shows a quiet conversation", locale)
		}
		if !strings.Contains(page, `chat-favorite-toggle`) || !strings.Contains(page, w.add) {
			t.Errorf("%s: the conversation header has no star", locale)
		}

		row := lane3Page(t, locale, build("idle"))
		for _, want := range []string{w.add, w.moveFav, w.moveNew} {
			if !strings.Contains(row, want) {
				t.Errorf("%s: the row menu lacks %q", locale, want)
			}
		}
		if strings.Contains(row[strings.Index(row, `class="rail-row-menu"`):], w.remove) {
			t.Errorf("%s: an ordinary row offers to remove a favorite", locale)
		}
		starred := lane3Page(t, locale, build("news"))
		if menu := starred[strings.Index(starred, `class="rail-row-menu"`):]; !strings.Contains(menu, w.remove) || strings.Contains(menu, w.moveFav) {
			t.Errorf("%s: a favorite's menu should offer Remove from favorites and not Move to Favorites", locale)
		}

		section := lane3Page(t, locale, build("section:custom-1"))
		menu := section[strings.Index(section, `class="rail-row-menu"`):]
		for _, want := range []string{w.rename, w.del} {
			if !strings.Contains(menu, want) {
				t.Errorf("%s: the section menu lacks %q", locale, want)
			}
		}
		builtin := lane3Page(t, locale, build("section:channels"))
		if menu := builtin[strings.Index(builtin, `class="rail-row-menu"`):]; strings.Contains(menu, w.rename) || strings.Contains(menu, w.del) {
			t.Errorf("%s: the built-in section can be renamed or deleted", locale)
		}
	}
}
