package values

import "testing"

func TestCanonicalLanguageTag(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  string
		valid bool
	}{
		{"en-us", "en-US", true},
		{"de-DE", "de-DE", true},
		{"und", "und", true},
		{"not-a-valid-locale-!", "", false},
	} {
		got, ok := CanonicalLanguageTag(tc.input)
		if got != tc.want || ok != tc.valid {
			t.Errorf("CanonicalLanguageTag(%q) = (%q, %v), want (%q, %v)", tc.input, got, ok, tc.want, tc.valid)
		}
	}
}
