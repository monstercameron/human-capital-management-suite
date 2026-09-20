package productui

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

var numericUSDate = regexp.MustCompile(`\b\d{2}/\d{2}/\d{4}\b`)

// TestDateVocabularyIsOneFormPerLocale pins the product formatter's civil
// date: English reads "19 Sep 2026" like the journey and Home pages, never
// the ambiguous numeric "09/19/2026"; German and Arabic keep their own
// convention; the context's time zone is still applied.
func TestDateVocabularyIsOneFormPerLocale(t *testing.T) {
	instant := time.Date(2026, time.September, 19, 13, 56, 0, 0, time.UTC)
	for locale, want := range map[string]string{
		"en-US": "19 Sep 2026",
		"de-DE": "19.09.2026",
		"ar":    "١٩ سبتمبر ٢٠٢٦",
	} {
		if got := ResolveProductLocale(locale).FormatDate(instant); got != want {
			t.Fatalf("%s FormatDate = %q, want %q", locale, got, want)
		}
	}
	late := time.Date(2026, time.September, 19, 23, 30, 0, 0, time.UTC)
	tokyo := ResolveProductLocale("en-US")
	tokyo.TimeZone = "Asia/Tokyo"
	if got := tokyo.FormatDate(late); got != "20 Sep 2026" {
		t.Fatalf("time zone ignored: %q", got)
	}
	broken := ResolveProductLocale("en-US")
	broken.TimeZone = "No/Such_Zone"
	if got := broken.FormatDate(late); got != "19 Sep 2026" {
		t.Fatalf("unknown zone did not fall back to UTC: %q", got)
	}
}

// TestHireDateUsesTheProductDateVocabulary is the Myself/Person regression:
// the employment overview printed "Hire date 02/23/2025".
func TestHireDateUsesTheProductDateVocabulary(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	got := localizedRecordDate(locale, "2025-02-23")
	if got != "23 Feb 2025" || numericUSDate.MatchString(got) {
		t.Fatalf("hire date = %q, want 23 Feb 2025", got)
	}
	section := ResolveWorkerOverview(locale, Person{ID: "w", Name: "Amara", HireDate: "2025-02-23"}, nil)
	rendered := ""
	for _, fact := range section.Facts {
		rendered += fact.Value + " "
	}
	if !strings.Contains(rendered, "23 Feb 2025") || numericUSDate.MatchString(rendered) {
		t.Fatalf("worker overview facts = %q", rendered)
	}
}

// TestWorkflowHistoryEffectiveDateAndYearFilter covers the History and Past
// workflows rows ("Effective · 2026-12-01") and the profile's year filter,
// which fixed desktop tracks cut to "Any effective".
func TestWorkflowHistoryEffectiveDateAndYearFilter(t *testing.T) {
	if got := historyEffectiveDateLabel(ResolveProductLocale("en-US"), "2026-12-01"); got != "1 Dec 2026" {
		t.Fatalf("history effective date = %q, want 1 Dec 2026", got)
	}
	if got := historyEffectiveDateLabel(ResolveProductLocale("en-US"), "not a date"); got != "not a date" {
		t.Fatalf("an unparseable stored value was lost: %q", got)
	}
	css := Stylesheet()
	if !strings.Contains(css, "@media (min-width:1200px){.history-filter-controls{grid-auto-columns:minmax(0,max-content);grid-auto-flow:column;}}") {
		t.Fatal("desktop history filters no longer size each select to its own options")
	}
	if strings.Contains(css, "minmax(145px,0.7fr)") {
		t.Fatal("the fixed desktop tracks that truncated the year select are back")
	}
}
