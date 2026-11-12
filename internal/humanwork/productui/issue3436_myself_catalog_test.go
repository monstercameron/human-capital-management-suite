package productui

import (
	"strings"
	"testing"
)

// TestIssue3436_MyselfPayrollAndHistorySectionsAreLocalized proves that the
// product-owned payroll and workflow-history copy on Myself is translated in
// every supported non-English catalog rather than falling back to English.
func TestIssue3436_MyselfPayrollAndHistorySectionsAreLocalized(t *testing.T) {
	keys := []string{
		"myself.payroll_title",
		"myself.payroll_detail",
		"myself.payroll_boundary",
		"myself.history_title",
		"myself.history_detail",
	}
	english := ResolveProductLocale("en-US")
	for _, code := range []string{"de-DE", "ar"} {
		locale := ResolveProductLocale(code)
		for _, key := range keys {
			got := locale.Text(key)
			if got == "" || strings.Contains(got, "⟦") || got == english.Text(key) {
				t.Fatalf("locale %s key %s is not translated: %q", code, key, got)
			}
		}
	}
}

// TestIssue3436_ArabicMyselfRendersTranslatedSections renders the served Arabic
// Myself page and checks the payroll and workflow-history headings and
// descriptions in Arabic, not their English catalog text.
func TestIssue3436_ArabicMyselfRendersTranslatedSections(t *testing.T) {
	view := testView(PageMyself)
	view.Viewer = ViewerProfile{PersonID: "worker-avery", Name: "Avery Patel", Initials: "AP"}
	view = ApplyLocale(view, ResolveProductLocale("ar"))
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"myself.payroll_title", "myself.history_title", "myself.history_detail", "myself.payroll_boundary"} {
		if want := view.Locale.Text(key); !strings.Contains(doc, want) {
			t.Fatalf("Arabic Myself missing translated %s: %q", key, want)
		}
	}
	for _, english := range []string{"Payroll &amp; compensation", "My workflow history", "Pay statements, deductions, taxes, bank details, and pay schedules are not available here yet"} {
		if strings.Contains(doc, english) {
			t.Fatalf("Arabic Myself rendered English product copy: %q", english)
		}
	}
}
