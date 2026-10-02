package chatui

import "testing"

// TestTodo_CHATLANG_005_Browser: a source in another language is named with its
// language in the reader's own words; one in the reader's language, or in no
// language, is left as it is.
func TestTodo_CHATLANG_005_Browser(t *testing.T) {
	for _, c := range []struct{ locale, title, language, want string }{
		{"de-DE", "Paid time off policy", "en", "Paid time off policy, auf Englisch"},
		{"ar", "Paid time off policy", "en", "Paid time off policy، باللغة الإنجليزية"},
		{"fr-FR", "Politique de congés", "en", "Politique de congés, in English"},
		{"en-US", "Paid time off policy", "de", "Paid time off policy, in German"},
		{"en-US", "Paid time off policy", "en", "Paid time off policy"},
		{"en-US", "Name only", "und", "Name only"},
		{"en-US", "Name only", "", "Name only"},
		{"en-US", "Name only", "sw", "Name only"},
	} {
		if got := ChatlangSourceLabel(c.locale, c.title, c.language); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.locale, c.language, got, c.want)
		}
	}
}
