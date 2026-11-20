package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/localize"
)

// Round 3 C-2: the day dividers and the member "active on" line wrote dates
// with Go's English layouts whatever the locale, so de-DE and ar showed
// "Tuesday, September 22" beside "Gestern" / "أمس". These tables follow the
// CLDR long and medium date patterns for the three product locales.
var (
	deWeekdays = [...]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"}
	arWeekdays = [...]string{"الأحد", "الاثنين", "الثلاثاء", "الأربعاء", "الخميس", "الجمعة", "السبت"}
)

// dateLocale reduces a BCP 47 tag to the date family this package formats.
func dateLocale(locale string) string {
	switch l := strings.ToLower(locale); {
	case strings.HasPrefix(l, "de"):
		return "de"
	case strings.HasPrefix(l, "ar"):
		return "ar"
	}
	return "en"
}

// arabicDigits swaps ASCII digits for Arabic-Indic ones, as the ar locale
// writes its dates.
func arabicDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			r = '٠' + (r - '0')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// formatDay writes a full day label: "Tuesday, September 22" (en),
// "Dienstag, 22. September" (de), "الثلاثاء، ٢٢ سبتمبر" (ar). With
// withYear the weekday is dropped and the year added, for days outside the
// current year.
func formatDay(locale string, t time.Time, withYear bool) string {
	return localize.FormatDay(locale, t, withYear)
}

// formatShortDate writes a compact month-day label: "Sep 22" (en),
// "22. Sept." (de), "٢٢ سبتمبر" (ar).
func formatShortDate(locale string, t time.Time) string {
	return localize.FormatShortDay(locale, t)
}
