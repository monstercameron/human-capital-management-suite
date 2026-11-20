package chatui

import (
	"strings"
	"testing"
)

func TestIssue3948_DiscardQuestionIsPrivateAndLocalized(t *testing.T) {
	for _, tc := range []struct{ locale, title, keep, yes string }{
		{"en-US", "Discard this report?", "Keep writing", "Discard report"},
		{"de-DE", "Diesen Bericht verwerfen?", "Weiterschreiben", "Bericht verwerfen"},
		{"ar", "تجاهل هذا البلاغ؟", "متابعة الكتابة", "تجاهل البلاغ"},
	} {
		markup := ReportDiscardMarkup(tc.locale)
		for _, want := range []string{`role="alertdialog"`, `aria-labelledby="chatremove-discard-title"`, `data-chatremove-discard="keep"`, `data-chatremove-discard="yes"`, tc.title, tc.keep, tc.yes} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s discard question misses %q in %s", tc.locale, want, markup)
			}
		}
		if strings.Index(markup, `data-chatremove-discard="keep"`) > strings.Index(markup, `data-chatremove-discard="yes"`) {
			t.Errorf("%s: Keep writing must come first", tc.locale)
		}
	}
	if body := ReportDiscardText("en-US", "discard_body"); !strings.Contains(body, "not been sent") || !strings.Contains(body, "saved or shared") {
		t.Errorf("the question does not say what is retained: %q", body)
	}
	if !strings.Contains(ReportDiscardMarkup("ar"), `dir="rtl"`) {
		t.Error("the Arabic question is not right to left")
	}
}
