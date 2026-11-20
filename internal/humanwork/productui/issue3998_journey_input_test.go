package productui

import (
	"html"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The slot is empty and hidden until the host knows this viewer holds the
// decision or has asked something, so it never says anything by itself.
func TestIssue3998_InputSlotIsHiddenAndEmptyUntilFilled(t *testing.T) {
	markup, err := ui.RenderToString(ProjectJourneyInput("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-projectui-input="journey:current"`, "hidden", `aria-live="polite"`, `data-text-ask="Ask for input"`, `data-text-label_informal="Informal input, not an approval"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, "<button") || strings.Contains(markup, "<li") || strings.Contains(markup, "journey=") {
		t.Fatalf("the slot offered or named something before the server answered: %s", markup)
	}
}

func TestIssue3998_InputCopyInEveryLocale(t *testing.T) {
	for key, words := range journeyInputWords {
		for i, locale := range SupportedProductLocales() {
			text := JourneyInputText(locale, key)
			if strings.TrimSpace(text) == "" {
				t.Errorf("%s: no %s", locale, key)
			}
			if i > 0 && text == words[0] && key != "cancel" {
				t.Errorf("%s: %s is not translated: %q", locale, key, text)
			}
		}
		for _, ph := range []string{"{name}", "{revision}", "{worker}", "{size}"} {
			base := strings.Contains(words[0], ph)
			for _, locale := range SupportedProductLocales() {
				if strings.Contains(JourneyInputText(locale, key), ph) != base {
					t.Errorf("%s: %s differs in %s", key, ph, locale)
				}
			}
		}
	}
	if JourneyInputText("fr-FR", "ask") != "Ask for input" || JourneyInputText("en-US", "nope") != "" {
		t.Fatal("an unsupported locale must read English and an unknown key must read empty")
	}
	markup, err := ui.RenderToString(ProjectJourneyInput("ar"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `dir="rtl"`) || !strings.Contains(html.UnescapeString(markup), "طلب رأي") {
		t.Errorf("the Arabic slot is not right to left or not Arabic: %s", markup)
	}
}

// Words a reviewer reads must say that advice is not an approval, and the
// label that travels with the question must say so in every language.
func TestIssue3998_InputCopySaysAdviceIsNeverAnApproval(t *testing.T) {
	if !strings.Contains(JourneyInputText("en-US", "informal_note"), "not an approval") || !strings.Contains(JourneyInputText("en-US", "informal_note"), "never approves") {
		t.Fatal("the note must say a reply never approves and that this is not an approval")
	}
	for _, locale := range SupportedProductLocales() {
		for _, key := range []string{"informal_note", "label_informal", "sent", "state_current"} {
			if JourneyInputText(locale, key) == "" {
				t.Errorf("%s: %s is missing", locale, key)
			}
		}
	}
	if !strings.Contains(JourneyInputText("en-US", "state_outdated"), "no longer apply") || !strings.Contains(JourneyInputText("en-US", "state_unavailable"), "not current") {
		t.Fatal("changed-proposal and lost-access states must say the advice may not be current")
	}
}
