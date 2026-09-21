package productui

import (
	"strings"
	"time"
)

// productDayMonthYearLayout is the product's English civil-date form
// ("19 Sep 2026"), the one the journey and Home surfaces already print
// (UXLIVE-016). The shared localizer's en-US form is the numeric
// "09/19/2026", which is ambiguous to a reader and was a second vocabulary
// on the same pages (hire date on Myself and Person, the guardrail's range
// date).
const productDayMonthYearLayout = "2 Jan 2006"

// productDayMonthYearLocale reports whether the product prints dates for
// this locale in the English day-month-year form. Other locales keep the
// shared localizer's own convention (de-DE "19.09.2026", Arabic month names).
func productDayMonthYearLocale(locale string) bool {
	locale = strings.ToLower(strings.TrimSpace(locale))
	return locale == "en" || strings.HasPrefix(locale, "en-")
}

// formatProductDayMonthYear renders value in the context's time zone, the
// same zone the shared localizer applies; an unknown zone falls back to UTC.
func formatProductDayMonthYear(c LocaleContext, value time.Time) string {
	location := time.UTC
	if zone := strings.TrimSpace(c.TimeZone); zone != "" {
		if loaded, err := time.LoadLocation(zone); err == nil {
			location = loaded
		}
	}
	return value.In(location).Format(productDayMonthYearLayout)
}
