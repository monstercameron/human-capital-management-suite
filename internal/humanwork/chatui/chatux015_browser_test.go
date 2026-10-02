package chatui_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATUX_015_Browser: Create conversation and Browse channels with the
// product's own catalog in each language, kinds explained by their use, the
// section arrows named, and no copy key.
func TestTodo_CHATUX_015_Browser(t *testing.T) {
	type words struct{ private, privateNote, group, groupNote, up, purpose string }
	for locale, w := range map[string]words{
		"en-US": {"Private channel", "A lasting topic for invited people.", "Group message", "A quick conversation with a few people.", "Move section up", "Design reviews"},
		"de-DE": {"Privater Kanal", "Ein dauerhaftes Thema für eingeladene Personen.", "Gruppennachricht", "Eine kurze Unterhaltung mit wenigen Personen.", "Abschnitt nach oben", "Design reviews"},
		"ar":    {"قناة خاصة", "موضوع دائم للأشخاص المدعوين.", "رسالة جماعية", "محادثة سريعة مع عدد قليل من الأشخاص.", "", "Design reviews"},
	} {
		create := lane3Page(t, locale, func(m *chatui.Model) { m.ShowCreate = true })
		lane3NoLeaks(t, locale+" create", create)
		for _, want := range []string{w.private, w.privateNote, w.group, w.groupNote} {
			if !strings.Contains(create, want) {
				t.Errorf("%s: Create conversation misses %q", locale, want)
			}
		}
		browse := lane3Page(t, locale, func(m *chatui.Model) {
			m.ShowBrowse = true
			m.Callbacks.JoinConversation, m.Callbacks.CloseBrowse = func(string) {}, func() {}
			m.Browse = []chatui.Conversation{{ID: "design", Name: "design", Kind: chatui.PublicChannel, Topic: "Design reviews and critique", MemberCount: 12}}
		})
		lane3NoLeaks(t, locale+" browse", browse)
		if !strings.Contains(browse, w.purpose) || !strings.Contains(browse, `data-action="join" data-id="design"`) {
			t.Errorf("%s: Browse channels misses the purpose or Join", locale)
		}
		page := lane3Page(t, locale, func(m *chatui.Model) {
			m.Callbacks.ReorderSection = func(string, int) {}
			m.Sections = []chatui.SidebarSection{{ID: "channels", Name: "Channels", Chats: m.Conversations}, {ID: "direct", Name: "Direct messages"}}
			m.RailMenuID = "section:direct"
		})
		// The hover arrows are gone (CHATSIDE-001): each heading has one named
		// three-dots button with a tooltip, and the moves are named menu items.
		if w.up != "" && !strings.Contains(page, ">"+w.up) {
			t.Errorf("%s: the section menu has no %q item", locale, w.up)
		}
		if strings.Contains(page, `class="section-order"`) || strings.Count(page, `class="rail-row-more section-more"`) != 2 || strings.Count(page, `title="`) < 2 {
			t.Errorf("%s: the section headings do not carry one named menu button each", locale)
		}
	}
}
