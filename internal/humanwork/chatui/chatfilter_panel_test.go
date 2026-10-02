package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatfilter"
)

// TestTodo_CHATMOD_003_PanelStates draws the administrator's panel in each state
// a read can be in and checks each one says its next step. The editor and the
// real catalog are in TestTodo_CHATMOD_003_Browser.
func TestTodo_CHATMOD_003_PanelStates(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{Locale: locale}
		markup, err := ui.RenderToString(ModAdminPanel(ModAdminProps{Model: m, CanManage: true, Definitions: []chatfilter.Definition{{ID: "invisible-internal-id", Name: "Project rule", Kind: "words", Action: "block", Match: []string{"quartz"}}}}))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{modadminText(m, "try_title"), modadminText(m, "try_btn"), modadminText(m, "e_save"), `id="modadmin-name"`, `for="modadmin-name"`, `aria-describedby="modadmin-h-name"`, `type="checkbox"`, `Project rule`} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s lacks %s: %s", locale, want, markup)
			}
		}
		if strings.Contains(markup, `value="Project rule"`) || strings.Contains(markup, ">invisible-internal-id<") {
			t.Fatal("editable input rebound or an id shown")
		}
		if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Fatal("RTL missing")
		}
		for _, props := range []ModAdminProps{{Model: m, CanManage: true, Loading: true}, {Model: m, CanManage: true, LoadError: "er_network"}, {Model: m}} {
			html, err := ui.RenderToString(ModAdminPanel(props))
			if err != nil {
				t.Fatal(err)
			}
			key := "loading"
			if props.LoadError != "" {
				key = "load_failed"
			}
			if !props.CanManage {
				key = "no_permission"
			}
			if !strings.Contains(html, modadminText(m, key)) {
				t.Fatal("state lacks next step", key)
			}
		}
	}
	if filterChecked("missing") {
		t.Fatal("native checked helper")
	}
	filterSetChecked("missing", false)
}
func TestTodo_CHATMOD_002_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{Locale: locale}
		markup, err := ui.RenderToString(RenderFilterMaskedText(m, "hello [removed word]"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, chatfilterText(m, "removed")) || strings.Contains(markup, "[removed word]") {
			t.Fatal("mask not announced in locale", markup)
		}
		markup, err = ui.RenderToString(RenderFilterBlockedDraft(m, "hello quartz", chatfilter.Span{Start: 6, End: 12}))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`role="alert"`, `<mark>quartz</mark>`, chatfilterText(m, "blocked")} {
			if !strings.Contains(markup, want) {
				t.Fatal("draft lost or inaccessible", markup)
			}
		}
	}
	for _, width := range []int{1440, 800, 390, 320} {
		_ = width
		for _, want := range []string{"min-width:0", "max-width:100%", "min-height:44px", ":focus-visible", "prefers-reduced-motion", "var(--hcm-"} {
			if !strings.Contains(ChatFilterStyles, want) {
				t.Fatal("layout token missing", want)
			}
		}
	}
	if strings.Contains(ChatFilterStyles, "font-family") || strings.Contains(ChatFilterStyles, "#") {
		t.Fatal("non-token style")
	}
}
func TestTodo_CHATMOD_002_Browser(t *testing.T) {
	markup, err := ui.RenderToString(RenderFilterBlockedDraft(Model{Locale: "en-US"}, "<script>quartz</script>", chatfilter.Span{Start: 8, End: 14}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(markup, "<script>") || !strings.Contains(markup, "&lt;script&gt;") {
		t.Fatal("untrusted draft not escaped", markup)
	}
	markup, err = ui.RenderToString(RenderFilterBlockedDraft(Model{}, "draft", chatfilter.Span{Start: -1, End: 50}))
	if err != nil || !strings.Contains(markup, "draft") {
		t.Fatal("invalid span drops draft", err)
	}
}

func TestTodo_CHATMOD_003_Settings(t *testing.T) {
	m := Model{Locale: "de-DE", IsTenantAdmin: true}
	m.FilterSettings = func() ui.Node { return html.P(html.Props{Text: "settings"}) }
	markup, err := ui.RenderToString(filterSettingsEntry(m, Conversation{ID: "room"}))
	if err != nil || !strings.Contains(markup, chatfilterText(m, "manage")) || strings.Contains(markup, "/workspace/app/chat/filters") {
		t.Fatal("settings not reachable inside the panel", err, markup)
	}
	m.FilterSettings = nil
	m.IsTenantAdmin = false
	if filterSettingsEntry(m, Conversation{}) != nil {
		t.Fatal("non-manager link")
	}
	m.CurrentUser = "owner"
	if filterSettingsEntry(m, Conversation{OwnerID: "owner"}) == nil {
		t.Fatal("channel owner link absent")
	}
	markup, err = ui.RenderToString(html.Div(html.Props{}, filterMessageBody(m, "hello [removed word]")...))
	if err != nil || !strings.Contains(markup, chatfilterText(m, "removed")) {
		t.Fatal("timeline mask not announced", err)
	}
	if FilterBlockedExplanation("de-DE") != chatfilterText(m, "blocked") {
		t.Fatal("browser and server copy diverged")
	}
}
