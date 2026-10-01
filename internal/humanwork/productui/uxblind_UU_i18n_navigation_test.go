package productui

import "testing"

func TestUU_PossessiveUsesLocaleRules(t *testing.T) {
	for _, tc := range []struct {
		locale, name, want string
	}{
		{"en-US", "Ana Flores", "Ana Flores'"},
		{"en-US", "Ana Brown", "Ana Brown's"},
		{"de-DE", "Ana Flores", "Ana Flores"},
		{"ar", "Ana Flores", "Ana Flores"},
	} {
		if got := ResolveProductLocale(tc.locale).Possessive(tc.name); got != tc.want {
			t.Errorf("%s possessive(%q) = %q, want %q", tc.locale, tc.name, got, tc.want)
		}
	}
}
