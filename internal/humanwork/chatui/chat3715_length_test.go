package chatui

import (
	"strings"
	"testing"
)

// TestIssue3715_OverlongComposerBodyIsSaidNotSilent pins the composer's length
// refusal: a body over the 4000-character server bound yields a visible notice
// in every supported locale, a body at the bound yields none, and multi-byte
// characters are counted as characters, not bytes.
func TestIssue3715_OverlongComposerBodyIsSaidNotSilent(t *testing.T) {
	cases := []struct {
		name   string
		locale string
		body   string
		want   bool
	}{
		{"en at bound", "en-US", strings.Repeat("a", 4000), false},
		{"en one over", "en-US", strings.Repeat("a", 4001), true},
		{"en 4500 chars", "en-US", strings.Repeat("L", 4500), true},
		{"de one over", "de-DE", strings.Repeat("ä", 4001), true},
		{"ar at bound counts runes not bytes", "ar", strings.Repeat("ب", 4000), false},
		{"ar one over", "ar", strings.Repeat("ب", 4001), true},
		{"empty", "en-US", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := chat3715LengthNotice(tc.locale, tc.body)
			if (got != "") != tc.want {
				t.Fatalf("chat3715LengthNotice(%q, %d runes) = %q, want notice=%v", tc.locale, len([]rune(tc.body)), got, tc.want)
			}
			if tc.want && !strings.Contains(got, "4000") {
				t.Fatalf("notice must state the 4000-character limit, got %q", got)
			}
		})
	}
}

// TestIssue3715_LengthNoticeLocalesAreTranslated refuses the key echo and an
// English fallback in de-DE and ar: each locale gets its own sentence.
func TestIssue3715_LengthNoticeLocalesAreTranslated(t *testing.T) {
	long := strings.Repeat("x", 4001)
	en := chat3715LengthNotice("en-US", long)
	de := chat3715LengthNotice("de-DE", long)
	ar := chat3715LengthNotice("ar", long)
	if en == "" || de == "" || ar == "" {
		t.Fatalf("every locale needs a notice, got en=%q de=%q ar=%q", en, de, ar)
	}
	if de == en || ar == en {
		t.Fatalf("de and ar must not fall back to English: en=%q de=%q ar=%q", en, de, ar)
	}
	if strings.Contains(de, "chat3715") || strings.Contains(ar, "chat3715") {
		t.Fatalf("a key leaked into the copy: de=%q ar=%q", de, ar)
	}
}
