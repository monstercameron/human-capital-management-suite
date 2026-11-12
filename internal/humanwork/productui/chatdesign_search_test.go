package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// #3775: on a Chat route the workspace search says what it finds (pages and
// people) in every locale; on every other page it keeps the workspace name.
func TestChatDesign_3775_WorkspaceSearchIsLabelledByScopeOnChat(t *testing.T) {
	for locale, want := range map[string]struct{ chat, elsewhere string }{
		"en-US": {"Search pages and people", "Search workspace"},
		"de-DE": {"Seiten und Personen durchsuchen", "Arbeitsbereich durchsuchen"},
		"ar":    {"البحث في الصفحات والأشخاص", "البحث في مساحة العمل"},
	} {
		for page, placeholder := range map[PageID]string{PageChat: want.chat, PageHome: want.elsewhere, PageAgents: want.elsewhere} {
			view := testView(page)
			view.Locale = ResolveProductLocalePreference(locale, "")
			markup, err := ui.RenderToString(ui.CreateElement(GlobalSearch, globalSearchProps(view)))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(markup, `placeholder="`+placeholder+`"`) {
				t.Errorf("%s %s: the workspace search does not say %q: %s", locale, page, placeholder, markup)
			}
			// The accessible name is the field's own and does not change with the route.
			if !strings.Contains(markup, `aria-label="`+view.Locale.Text("global_search.label")+`"`) {
				t.Errorf("%s %s: the workspace search lost its accessible name", locale, page)
			}
		}
		if want.chat == want.elsewhere {
			t.Errorf("%s: the Chat placeholder does not differ from the workspace one", locale)
		}
	}
}

// On a Chat route the workspace search is the secondary search: short and
// muted at rest, full again with focus, and its boundary keeps control contrast.
func TestChatDesign_3775_WorkspaceSearchIsLighterOnChat(t *testing.T) {
	css := Stylesheet()
	for _, want := range []string{
		`.app-shell:has(.chat-workspace) .global-search:not(:focus-within){max-inline-size:320px}`,
		`.app-shell:has(.chat-workspace) .global-search:not(:focus-within) .global-search-input{background:transparent}`,
		`.app-shell:has(.chat-workspace) .global-search:not(:focus-within) .global-search-input::placeholder{color:var(--muted)}`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet is missing %q", want)
		}
	}
	if !strings.Contains(ChatStylesheet(), chatDesignSearchStylesheet) {
		t.Error("the rule is not in the sheet a Chat route is served")
	}
	// Scoped to Chat: every selector reads the route, none is a bare shell rule.
	for _, selector := range strings.Split(chatDesignSearchStylesheet, "}") {
		for _, part := range strings.Split(strings.SplitN(selector, "{", 2)[0], ",") {
			if part = strings.TrimSpace(part); part != "" && !strings.HasPrefix(part, ".app-shell:has(.chat-workspace) ") {
				t.Errorf("%q also applies outside Chat", part)
			}
		}
	}
	if strings.Contains(chatDesignSearchStylesheet, "border") || strings.Contains(chatDesignSearchStylesheet, "outline") {
		t.Error("the lighter search touches the field's boundary or focus ring")
	}
	// The muted placeholder is still readable text in both themes.
	for _, dark := range []bool{false, true} {
		vars := chatDesignThemeVars(t, dark)
		if ratio := chatDesignContrast(t, vars, "var(--muted)", "var(--surface)"); ratio < 4.5 {
			t.Errorf("dark=%v: the muted placeholder is %.2f:1", dark, ratio)
		}
	}
}
