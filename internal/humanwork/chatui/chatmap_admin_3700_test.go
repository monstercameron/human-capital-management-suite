package chatui

import (
	"strings"
	"testing"
)

// ISSUE-3700: a country the server refuses with 400 is a field-level error on
// the country form, not the page-level "could not load or save" line.
func TestIssue3700_CountryRefusalIsFieldLevel(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := chatmapAdminLoaded()
		m.Locale = locale
		m.CountryRejected = true
		markup := renderNode(t, ChatmapWorkspaceSettings(m))
		if !strings.Contains(markup, ChatmapAdminText(locale, "admin_country_refused")) {
			t.Fatalf("%s: rejected-country message missing: %s", locale, markup)
		}
		if strings.Contains(markup, ChatmapAdminText(locale, "admin_error")) {
			t.Fatalf("%s: page-level failure still shown for a refused country", locale)
		}
		if !strings.Contains(markup, `aria-invalid="true"`) {
			t.Fatalf("%s: country input is not marked invalid", locale)
		}
	}
	plain := renderNode(t, ChatmapWorkspaceSettings(chatmapAdminLoaded()))
	if strings.Contains(plain, "aria-invalid") {
		t.Fatal("a valid country form is marked invalid")
	}
}

func TestIssue3700_OnlyCountryValidationRefusalIsFieldLevel(t *testing.T) {
	if !chatmapAdminCountryRefused("setjurisdiction", 400) {
		t.Fatal("a 400 on setjurisdiction must be a field-level refusal")
	}
	for _, c := range []struct {
		action string
		status int
	}{{"setjurisdiction", 503}, {"setjurisdiction", 403}, {"setjurisdiction", 401}, {"setpolicy", 400}, {"settings", 400}} {
		if chatmapAdminCountryRefused(c.action, c.status) {
			t.Fatalf("%s %d must stay page-level", c.action, c.status)
		}
	}
}
