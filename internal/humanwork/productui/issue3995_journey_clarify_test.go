package productui

import (
	"html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The slot is empty and hidden until the host knows this viewer holds the
// decision or is part of a question, so it never says anything by itself.
func TestIssue3995_ClarifySlotIsHiddenAndEmptyUntilFilled(t *testing.T) {
	markup, err := ui.RenderToString(ProjectJourneyClarify("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-projectui-clarify="journey:current"`, "hidden", `aria-live="polite"`, `data-text-ask="Ask for clarification"`, `data-text-audience=`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, "<button") || strings.Contains(markup, "<li") || strings.Contains(markup, "journey=") {
		t.Fatalf("the slot offered or named something before the server answered: %s", markup)
	}
}

func TestIssue3995_ClarifyCopyInEveryLocale(t *testing.T) {
	for key, words := range journeyClarifyWords {
		for i, locale := range SupportedProductLocales() {
			text := JourneyClarifyText(locale, key)
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s: no %s", locale, key)
			}
			if i > 0 && text == words[0] && key != "cancel" {
				t.Errorf("%s: %s is not translated: %q", locale, key, text)
			}
		}
		// The same placeholders in every language, so the host can fill any of them.
		for _, ph := range []string{"{name}", "{revision}", "{worker}"} {
			base := strings.Contains(words[0], ph)
			for _, locale := range SupportedProductLocales() {
				if strings.Contains(JourneyClarifyText(locale, key), ph) != base {
					t.Errorf("%s: %s differs in %s", key, ph, locale)
				}
			}
		}
	}
	if JourneyClarifyText("fr-FR", "ask") != "Ask for clarification" || JourneyClarifyText("en-US", "nope") != "" {
		t.Fatal("an unsupported locale must read English and an unknown key must read empty")
	}
	markup, err := ui.RenderToString(ProjectJourneyClarify("ar"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `dir="rtl"`) || !strings.Contains(html.UnescapeString(markup), "طلب توضيح") {
		t.Errorf("the Arabic slot is not right to left or not Arabic: %s", markup)
	}
}

// Words an approver reads must not promise more than the feature does.
func TestIssue3995_ClarifyCopySaysAReplyChangesNothing(t *testing.T) {
	for _, locale := range SupportedProductLocales() {
		if JourneyClarifyText(locale, "reply_note") == "" || JourneyClarifyText(locale, "audience") == "" {
			t.Errorf("%s: the audience or the reply note is missing", locale)
		}
	}
	if !strings.Contains(JourneyClarifyText("en-US", "reply_note"), "does not change, approve or submit") {
		t.Fatal("the reply note must say that a reply does not change, approve or submit")
	}
}
