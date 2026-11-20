package chatui

import (
	"strings"
	"testing"
)

// Issue 3820: the question editor labelled the retention field with the raw
// "%d days" template.
func TestIssue3820_RetentionLabelIsPlainWords(t *testing.T) {
	v := chatgateViewFixture()
	v.Administrator = true
	want := map[string]string{"en-US": "Keep answers for (days)", "de-DE": "Antworten aufbewahren (Tage)", "ar": "مدة الاحتفاظ بالإجابات (بالأيام)"}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		v.Locale = locale
		s := gateMarkup(t, v)
		if strings.Contains(s, "%d") {
			t.Fatalf("%s: editor shows an unfilled placeholder", locale)
		}
		if !strings.Contains(s, ">"+want[locale]+"<") {
			t.Fatalf("%s: retention label %q missing", locale, want[locale])
		}
		if !strings.Contains(s, `for="gate-editor-0-days"`) {
			t.Fatalf("%s: label is not tied to the input", locale)
		}
	}
}
