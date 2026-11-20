package chatui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func journeyRetryMarkup(t *testing.T, locale, state string, withCallback bool) string {
	t.Helper()
	m := Model{EmbedOrigin: "https://hcm.example", Locale: locale, JourneyPreviews: map[string]JourneyPreview{"01a0-abc": {IntentID: "01a0-abc", State: state}}}
	if withCallback {
		m.Callbacks.RetryJourney = func(string) {}
	}
	markup, err := ui.RenderToString(ui.Fragment(journeyPreviewEmbeds(m, "https://hcm.example/workspace/app/journeys?journey=01a0-abc")...))
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

// A temporary failure offers a labelled retry control; no-access and
// not-found do not (retrying cannot change them, and a button would turn the
// card into an existence oracle).
func TestIssue3729_TemporaryFailureOffersRetry(t *testing.T) {
	for locale, label := range map[string]string{"en-US": "Try again", "de-DE": "Erneut versuchen", "ar": "حاول مرة أخرى"} {
		markup := journeyRetryMarkup(t, locale, "unavailable", true)
		if !strings.Contains(markup, `data-action="retry-journey"`) || !strings.Contains(markup, `data-id="01a0-abc"`) {
			t.Errorf("%s: unavailable card has no retry control:\n%s", locale, markup)
		}
		if !strings.Contains(markup, label) {
			t.Errorf("%s: retry label %q missing:\n%s", locale, label, markup)
		}
	}
}

func TestIssue3729_RetryOnlyForTemporaryFailure(t *testing.T) {
	for _, state := range []string{"restricted", "missing", "loading"} {
		if markup := journeyRetryMarkup(t, "en-US", state, true); strings.Contains(markup, "retry-journey") {
			t.Errorf("state %q must not offer retry:\n%s", state, markup)
		}
	}
	if markup := journeyRetryMarkup(t, "en-US", "unavailable", false); strings.Contains(markup, "retry-journey") {
		t.Errorf("no callback wired: a dead retry button must not render:\n%s", markup)
	}
}

// While the retry reads, the button stays in place (disabled, not removed) so
// keyboard focus survives, and the card announces that it is loading.
func TestIssue3729_RetryingKeepsButtonInPlace(t *testing.T) {
	m := Model{EmbedOrigin: "https://hcm.example", Locale: "en-US", JourneyPreviews: map[string]JourneyPreview{"01a0-abc": {IntentID: "01a0-abc", State: "loading", Retrying: true}}}
	m.Callbacks.RetryJourney = func(string) {}
	markup, err := ui.RenderToString(ui.Fragment(journeyPreviewEmbeds(m, "https://hcm.example/workspace/app/journeys?journey=01a0-abc")...))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`data-action="retry-journey"`, `aria-disabled="true"`, `aria-busy="true"`, `role="status"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("retrying card lacks %s:\n%s", want, markup)
		}
	}
	m.JourneyPreviews["01a0-abc"] = JourneyPreview{IntentID: "01a0-abc", State: "loading"}
	plain, _ := ui.RenderToString(ui.Fragment(journeyPreviewEmbeds(m, "https://hcm.example/workspace/app/journeys?journey=01a0-abc")...))
	if strings.Contains(plain, "retry-journey") {
		t.Errorf("a first load must not show a retry button:\n%s", plain)
	}
}
