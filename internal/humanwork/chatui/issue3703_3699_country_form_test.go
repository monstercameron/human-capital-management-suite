package chatui

import (
	"strings"
	"testing"
)

// ISSUE-3699: the inline error for a refused country names the problem (not a
// known country, symbols) rather than only a length, in every language.
func TestIssue3699_RefusedCountryMessageNamesUnknownCountry(t *testing.T) {
	for locale, want := range map[string]string{"en-US": "not a country we know", "de-DE": "kein bekanntes Land", "ar": "ليس بلداً معروفاً"} {
		msg := ChatmapAdminText(locale, "admin_country_refused")
		if !strings.Contains(msg, want) {
			t.Fatalf("%s: refused message %q does not say the country is unknown", locale, msg)
		}
	}
}

// ISSUE-3703: the Basis field is required in the markup (the visible asterisk
// comes from the stylesheet, pinned by TestChat3703RequiredBasisMarkerIsInStylesheet).
func TestIssue3703_BasisInputIsRequired(t *testing.T) {
	markup := renderNode(t, ChatmapWorkspaceSettings(chatmapAdminLoaded()))
	i := strings.Index(markup, `id="chatmapadmin-basis"`)
	if i < 0 || !strings.Contains(markup[i:i+200], "required") {
		t.Fatalf("basis input is not required: %s", markup)
	}
}
