package chatui_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrender"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_087_Browser renders the reading-language form after a save
// that failed, in each language the product ships: the person's choice is still
// the selected language, the line says what failed in plain words (never a raw
// copy key), and "Try again" is on the form.
func TestTodo_CHATBUG_087_Browser(t *testing.T) {
	cases := []struct {
		locale, key string
		words       []string
	}{
		{"en-US", "error_signed_out", []string{"You were signed out", "Reload the page to sign in again", "Try again"}},
		{"en-US", "error_unreachable", []string{"The server did not answer", "Your choice is kept here", "Try again"}},
		{"en-US", "error_denied", []string{"You are not allowed to change this setting", "Try again"}},
		{"en-US", "", []string{"Settings could not be saved. Try again."}},
		{"de-DE", "error_signed_out", []string{"Sie wurden abgemeldet", "Erneut versuchen"}},
		{"ar", "error_signed_out", []string{"تم تسجيل خروجك", "حاول مرة أخرى"}},
	}
	for _, c := range cases {
		pref := chatrender.DefaultPreference("en")
		pref.ReadingLanguage, pref.Translate = "de", true
		page, err := ui.RenderToString(chatui.RenderingSettings(chatui.RenderingSettingsModel{Locale: c.locale, Preference: pref, AutoSave: true, Failed: true, FailKey: c.key}))
		if err != nil {
			t.Fatal(err)
		}
		page = html.UnescapeString(page)
		if strings.Contains(page, "⟦") {
			t.Errorf("%s/%s: the form prints a copy key: %s", c.locale, c.key, regexp.MustCompile(`.{30}⟦[^⟧]*⟧`).FindString(page))
		}
		for _, word := range c.words {
			if !strings.Contains(page, word) {
				t.Errorf("%s/%s: the failed form lacks %q", c.locale, c.key, word)
			}
		}
		if !regexp.MustCompile(`<option[^>]*value="de"[^>]*selected`).MatchString(page) && !regexp.MustCompile(`<option[^>]*selected[^>]*value="de"`).MatchString(page) {
			t.Errorf("%s/%s: the person's choice (German) is not the selected language", c.locale, c.key)
		}
		if !strings.Contains(page, `chatrender-retry`) {
			t.Errorf("%s/%s: no Try again button", c.locale, c.key)
		}
	}
	// A form that did not fail has no retry button and says nothing is wrong.
	page, err := ui.RenderToString(chatui.RenderingSettings(chatui.RenderingSettingsModel{Locale: "en-US", Preference: chatrender.DefaultPreference("en"), AutoSave: true}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page, "chatrender-retry") || strings.Contains(html.UnescapeString(page), "could not be saved") {
		t.Errorf("a form that did not fail shows a failure: %s", page)
	}
}

// TestTodo_CHATBUG_087_Browser_Details: the channel translation setting in
// Conversation details says the session ended when it did, and keeps the
// general line for any other failure.
func TestTodo_CHATBUG_087_Browser_Details(t *testing.T) {
	for _, c := range []struct {
		signedOut bool
		want      string
	}{
		{true, "You were signed out, so translation settings could not load or save. Reload the page to sign in again."},
		{false, "Translation settings could not load or save. Try again."},
	} {
		node := chatui.TranslationChannelRow(chatui.TranslationAdminModel{Locale: "en-US", Failed: true, SignedOut: c.signedOut}, func(string) string { return "" })
		page, err := ui.RenderToString(node)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(html.UnescapeString(page), c.want) {
			t.Errorf("signed out = %v: %q is not on the page: %s", c.signedOut, c.want, page)
		}
	}
}
