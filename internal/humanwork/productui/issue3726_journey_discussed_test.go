package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The slot is empty and hidden until the host has something the viewer may
// open, so it never says anything about a conversation by itself.
func TestIssue3726_DiscussedSlotIsHiddenAndEmptyUntilFilled(t *testing.T) {
	markup, err := ui.RenderToString(ProjectJourneyDiscussed("en-US"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-projectui-discussed="journey:current"`, "hidden", `aria-live="polite"`, `data-text-heading="Discussed in"`, `data-text-direct="Direct message"`, `data-text-open="Open conversation {name}"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q in %s", want, markup)
		}
	}
	if strings.Contains(markup, "<a ") || strings.Contains(markup, "<li") || strings.Contains(markup, "journey=") {
		t.Fatalf("the slot named a conversation or a journey before the server answered: %s", markup)
	}
}

func TestIssue3726_DiscussedSlotCopyInEveryLocale(t *testing.T) {
	seen := map[string]string{}
	for _, locale := range SupportedProductLocales() {
		markup, err := ui.RenderToString(ProjectJourneyDiscussed(locale))
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		for _, attr := range []string{"data-text-heading", "data-text-direct", "data-text-open", "data-text-incomplete"} {
			i := strings.Index(markup, attr+`="`)
			if i < 0 {
				t.Fatalf("%s: no %s in %s", locale, attr, markup)
			}
			rest := markup[i+len(attr)+2:]
			value := rest[:strings.Index(rest, `"`)]
			if strings.TrimSpace(value) == "" || strings.HasPrefix(value, "projectui.") {
				t.Errorf("%s: %s is %q", locale, attr, value)
			}
			if locale != "en-US" && value == seen[attr] {
				t.Errorf("%s: %s is not translated: %q", locale, attr, value)
			}
			if locale == "en-US" {
				seen[attr] = value
			}
		}
		if locale == "ar" && !strings.Contains(markup, `dir="rtl"`) {
			t.Errorf("ar slot is not right to left: %s", markup)
		}
	}
}
