package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestChat3729JourneyFailureCopyIsDistinct pins issue #3729: a viewer without
// a grant, a missing journey and a temporary failure must each read differently
// on the journey card, and the temporary failure must say to try again.
func TestChat3729JourneyFailureCopyIsDistinct(t *testing.T) {
	body := "Status: https://hcm.example/workspace/app/journeys?journey=01a0-abc"
	cases := map[string]string{
		"restricted":  "do not have access",
		"missing":     "not found",
		"unavailable": "Try again",
	}
	markups := map[string]string{}
	for state, want := range cases {
		m := Model{EmbedOrigin: "https://hcm.example", Locale: "en-US", JourneyPreviews: map[string]JourneyPreview{"01a0-abc": {IntentID: "01a0-abc", State: state}}}
		markup, err := ui.RenderToString(ui.Fragment(journeyPreviewEmbeds(m, body)...))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, want) {
			t.Errorf("state %q card missing %q:\n%s", state, want, markup)
		}
		if !strings.Contains(markup, `data-embed-state="`+state+`"`) {
			t.Errorf("state %q not exposed on the card:\n%s", state, markup)
		}
		markups[state] = markup
	}
	if markups["restricted"] == markups["missing"] || markups["missing"] == markups["unavailable"] {
		t.Fatal("no-access, not-found and temporary failure render the same card")
	}
	// The three copies are distinct from each other: each state's card must not
	// carry another state's sentence.
	for state, markup := range markups {
		for other, want := range cases {
			if other != state && strings.Contains(markup, want) {
				t.Errorf("state %q card carries %q copy", state, other)
			}
		}
	}
	m := Model{EmbedOrigin: "https://hcm.example", Locale: "de-DE", JourneyPreviews: map[string]JourneyPreview{"01a0-abc": {IntentID: "01a0-abc", State: "unavailable"}}}
	markup, _ := ui.RenderToString(ui.Fragment(journeyPreviewEmbeds(m, body)...))
	if strings.Contains(markup, "Try again") || !strings.Contains(markup, "erneut") {
		t.Errorf("de-DE temporary failure copy not localized:\n%s", markup)
	}
}
