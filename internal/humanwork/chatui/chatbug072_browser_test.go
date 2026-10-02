package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// lane3Page renders the whole chat page with the product's own catalog, the one
// that answers an unknown key with a marker, so a key the page forgot shows up.
func lane3Page(t *testing.T, locale string, change func(*chatui.Model)) string {
	t.Helper()
	ctx := productui.ResolveProductLocale(locale)
	room := chatui.Conversation{ID: "general", Name: "General Chat!", Kind: chatui.PublicChannel, OwnerID: "walt", MemberCount: 18, Joined: true}
	m := chatui.Model{State: chatui.StateReady, Locale: ctx.Resolved, Direction: string(ctx.Direction), SelectedID: room.ID, CurrentUser: "walt", CurrentTenantID: "t",
		Text:          func(key string) string { return ctx.Text(key) },
		Conversations: []chatui.Conversation{room, {ID: "sales", Name: "sales", Kind: chatui.PublicChannel, Joined: true}},
		Members:       []chatui.Member{{ID: "walt", HomeTenantID: "t", Name: "Walt Brennan"}, {ID: "jake", HomeTenantID: "t", Name: "Jake Sullivan"}},
		Callbacks: chatui.Callbacks{CreateConversation: func(chatui.ConversationKind, string, []string) {}, CloseCreate: func() {}, OpenCreate: func() {}, Search: func(string) {}, SelectConversation: func(string) {},
			OpenRailMenu: func(string) {}, ToggleDetails: func(bool) {}},
	}
	if change != nil {
		change(&m)
	}
	page, err := ui.RenderToString(chatui.Build(m))
	if err != nil {
		t.Fatal(err)
	}
	return html.UnescapeString(page)
}

func lane3NoLeaks(t *testing.T, where, markup string) {
	t.Helper()
	if strings.Contains(markup, "⟦") {
		t.Errorf("%s prints a copy key: %s", where, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(markup))
	}
	if found := regexp.MustCompile(`\bchat\.(?:bug|ux)\d+[a-z0-9_.]*`).FindString(markup); found != "" {
		t.Errorf("%s prints the key %s", where, found)
	}
	if strings.Contains(markup, ` style="`) || strings.Contains(markup, "<style") {
		t.Errorf("%s carries a style attribute or element", where)
	}
}

// TestTodo_CHATBUG_072_Browser: the create dialog prints the naming rule under
// the name box in every language, and the search bar offers to search only in a
// channel whose name holds a space and a mark, without rewriting the name.
func TestTodo_CHATBUG_072_Browser(t *testing.T) {
	hints := map[string]string{
		"en-US": "Lowercase, no spaces. Use dashes, like design-reviews.",
		"de-DE": "Kleinbuchstaben, ohne Leerzeichen. Bindestriche verwenden, z. B. design-reviews.",
		"ar":    "أحرف صغيرة بلا مسافات. استخدم الشرطات مثل design-reviews.",
	}
	offers := map[string]string{"en-US": "Search only in #General Chat!", "de-DE": "Nur in #General Chat! suchen", "ar": "البحث في #General Chat! فقط"}
	for locale, hint := range hints {
		create := lane3Page(t, locale, func(m *chatui.Model) { m.ShowCreate = true })
		lane3NoLeaks(t, locale+" create dialog", create)
		if !strings.Contains(create, hint) || !strings.Contains(create, `id="new-chat-name"`) || !strings.Contains(create, `aria-describedby="new-chat-name-hint"`) {
			t.Errorf("%s: the create dialog does not carry the rule under the name box", locale)
		}
		if strings.Contains(create, "field-error") {
			t.Errorf("%s: a fresh dialog already shows an error", locale)
		}
		search := lane3Page(t, locale, func(m *chatui.Model) { m.Search = "item" })
		lane3NoLeaks(t, locale+" search", search)
		if !strings.Contains(search, offers[locale]) {
			t.Errorf("%s: the scope chip is not an offer (%q)", locale, offers[locale])
		}
		// The offer names the channel the way the filter reads back.
		if !strings.Contains(search, `data-extra="&#34;General Chat!&#34;"`) && !strings.Contains(search, `data-extra="&quot;General Chat!&quot;"`) && !strings.Contains(search, `data-extra="`+`"General Chat!"`+`"`) {
			t.Errorf("%s: the chip does not quote the name", locale)
		}
	}
}
