package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
)

func chatrenderMarkup(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}
func TestTodo_CHATRENDER_001_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		mark := chatrender.Mark{State: "ready", Kinds: []chatrender.Kind{chatrender.Reword, chatrender.Translate, chatrender.Mask}, SourceLanguage: "de", CanShowOriginal: true, CanShowAsWritten: true}
		markup := chatrenderMarkup(t, RenderingView(locale, chatrender.Rendering{Text: `<script>alert("bad")</script>`}, mark, ui.Handler{}, ui.Handler{}))
		for _, want := range []string{RenderingText(locale, "reworded"), RenderingText(locale, "mask"), RenderingText(locale, "original"), RenderingText(locale, "written"), RenderingText(locale, "de"), "&lt;script&gt;", `dir="auto"`} {
			if !strings.Contains(markup, want) {
				t.Fatalf("%s missing %q: %s", locale, want, markup)
			}
		}
		if strings.Contains(markup, "<script>") {
			t.Fatal("untrusted rendering is HTML")
		}
		if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Fatal("missing RTL")
		}
		mark.CanShowOriginal = false
		mark.CanShowAsWritten = false
		markup = chatrenderMarkup(t, RenderingMark(locale, mark, ui.Handler{}, ui.Handler{}))
		if strings.Contains(markup, "<button") {
			t.Fatal("forbidden switch rendered")
		}
		for _, state := range []string{"pending", "fallback", "unavailable"} {
			markup = chatrenderMarkup(t, RenderingMark(locale, chatrender.Mark{State: state}, ui.Handler{}, ui.Handler{}))
			if !strings.Contains(markup, RenderingText(locale, state)) {
				t.Fatal(state, markup)
			}
		}
	}
	for _, required := range []string{"min-height:44px", "max-width:100%", "min-width:0", ":focus-visible", "prefers-reduced-motion", "--hcm-color-text", "--hcm-color-brand-primary", "--hcm-radius-control"} {
		if !strings.Contains(RenderingStyles, required) {
			t.Fatal("missing responsive/token rule", required)
		}
	}
	if strings.Contains(RenderingStyles, "#") || strings.Contains(RenderingStyles, "font-family") {
		t.Fatal("hard coded palette or font")
	}
}
func TestTodo_CHATLANG_002_Browser(t *testing.T) {
	testRenderingSettingsClient(t)
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		pref := chatrender.DefaultPreference(locale)
		pref.Translate = true
		pref.FurtherLanguages = []string{"fr"}
		pref.SourceOverrides = map[string]bool{"es": false}
		for _, state := range []string{"normal", "loading", "failed", "saved"} {
			model := RenderingSettingsModel{Locale: locale, Preference: pref, Conversation: true, Loading: state == "loading", Failed: state == "failed", Saved: state == "saved"}
			markup := chatrenderMarkup(t, RenderingSettings(model))
			for _, want := range []string{RenderingText(locale, "reading"), RenderingText(locale, "translate"), RenderingText(locale, "further"), RenderingText(locale, "source"), RenderingText(locale, "scope"), RenderingText(locale, "save"), `for="chatrender-reading"`, `aria-describedby="chatrender-settings-status"`, `name="further"`, `name="never"`} {
				if !strings.Contains(markup, want) {
					t.Fatalf("%s %s missing %q", locale, state, want)
				}
			}
			key := "help"
			switch state {
			case "loading":
				key = "loading"
			case "failed":
				key = "error"
			case "saved":
				key = "saved"
			}
			if !strings.Contains(markup, RenderingText(locale, key)) {
				t.Fatal(state, markup)
			}
		}
		markup := chatrenderMarkup(t, RenderingLanguageIndicator(locale, map[string]int{"de": 2, "en": 1, "ar": 0}))
		if !strings.Contains(markup, RenderingText(locale, "de")+": 2") || strings.Contains(markup, ": 0") {
			t.Fatal(markup)
		}
		markup = chatrenderMarkup(t, RenderingLanguageIndicator(locale, nil))
		if !strings.Contains(markup, RenderingText(locale, "empty")) {
			t.Fatal(markup)
		}
		for _, failed := range []bool{false, true} {
			markup = chatrenderMarkup(t, RenderingLanguageStatusIndicator(locale, nil, !failed, failed))
			key := "languages_loading"
			if failed {
				key = "languages_error"
			}
			if !strings.Contains(markup, RenderingText(locale, key)) {
				t.Fatal(markup)
			}
		}
	}
	markup := render(t, Model{Locale: "de-DE", State: StateReady})
	if !strings.Contains(markup, "Lesesprachen") || !strings.Contains(markup, `name="reading"`) {
		t.Fatal("personal settings hook absent")
	}
	if RenderingText("de-DE", "missing") != "Nicht angegeben" {
		t.Fatal("unknown key fallback")
	}
	if cleanup := renderingLoadSettings("", func(chatrender.Preference, error) { t.Fatal("native browser call") }); cleanup != nil {
		t.Fatal("native load side effect")
	}
	if cleanup := renderingLoadLanguages("", func(map[string]int, error) { t.Fatal("native browser call") }); cleanup != nil {
		t.Fatal("native language load side effect")
	}
	if _, _, err := renderingReadSettings(ui.Event{}, chatrender.Preference{}, ""); err != chatrender.ErrUnavailable {
		t.Fatal(err)
	}
	called := false
	renderingSaveSettings("", chatrender.Preference{}, func(err error) {
		called = true
		if err != chatrender.ErrUnavailable {
			t.Fatal(err)
		}
	})
	if !called {
		t.Fatal("native failure missing")
	}
}
