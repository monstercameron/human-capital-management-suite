package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// ISSUE-3700 / 3699: the refused-country message belongs at the Country field:
// inside the country form, announced as an alert, and named by the input's
// aria-describedby. It must not sit in the section's status line above the
// location settings, away from the field it is about.
func TestIssue3700_CountryErrorSitsAtTheField(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		for _, key := range []string{"refused", "invalid"} {
			m := chatmapAdminLoaded()
			m.Locale = locale
			want := ChatmapAdminText(locale, "admin_country_refused")
			if key == "refused" {
				m.CountryRejected = true
			} else {
				m.Invalid = true
				want = ChatmapAdminText(locale, "admin_country_invalid")
			}
			markup := renderNode(t, ChatmapWorkspaceSettings(m))
			form := markup[strings.Index(markup, `data-chatmapadmin="country"`):]
			if !strings.Contains(form, want) {
				t.Fatalf("%s/%s: message is not inside the country form: %s", locale, key, markup)
			}
			status := regexp.MustCompile(`id="chatmapadmin-status"[^>]*>([^<]*)<`).FindStringSubmatch(markup)
			if len(status) == 2 && strings.TrimSpace(status[1]) != "" {
				t.Fatalf("%s/%s: message duplicated in the section status line: %q", locale, key, status[1])
			}
			if !regexp.MustCompile(`id="chatmapadmin-country-error"[^>]*role="alert"|role="alert"[^>]*id="chatmapadmin-country-error"`).MatchString(form) {
				t.Fatalf("%s/%s: error is not an alert with its own id: %s", locale, key, form)
			}
			if !regexp.MustCompile(`aria-describedby="[^"]*chatmapadmin-country-error[^"]*"`).MatchString(form) {
				t.Fatalf("%s/%s: country input does not point at its error", locale, key)
			}
		}
	}
	plain := renderNode(t, ChatmapWorkspaceSettings(chatmapAdminLoaded()))
	if strings.Contains(plain, "chatmapadmin-country-error") {
		t.Fatal("a valid country form shows an error")
	}
}
