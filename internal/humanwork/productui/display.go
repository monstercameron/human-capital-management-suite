package productui

import (
	"strings"
	"time"
)

// DisplayLabel turns a stable machine identifier into neutral shell copy.
// It is intentionally conservative: business record names always come from
// the authorized service projection and never pass through this helper.
func DisplayLabel(value string) string {
	words := strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(strings.TrimSpace(value)))
	for index, word := range words {
		runes := []rune(strings.ToLower(word))
		if len(runes) > 0 {
			runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		}
		words[index] = string(runes)
	}
	return strings.Join(words, " ")
}

// FormatDisplayDate converts the canonical civil-date forms used by service
// projections into the product's one locale-aware date vocabulary. Invalid or
// already-human values are preserved so presentation never erases evidence.
func FormatDisplayDate(locale LocaleContext, value string) string {
	raw := strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02", time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return locale.FormatDate(parsed)
		}
	}
	return value
}

// FormatDisplayTimestamp applies the shared timestamp form to common service
// encodings, including the historical UTC display strings in journey data.
func FormatDisplayTimestamp(locale LocaleContext, value string) string {
	raw := strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2 Jan 2006, 15:04 MST", "2 Jan 2006 · 15:04 MST"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return locale.FormatTimestamp(parsed)
		}
	}
	return value
}

// LogicalArrow returns a directional text glyph whose visual direction
// follows the document's reading direction. forward means movement toward the
// next destination; false means back toward the previous destination.
func LogicalArrow(locale LocaleContext, forward bool) string {
	rtl := string(locale.normalized().Direction) == "rtl"
	if forward == rtl {
		return "←"
	}
	return "→"
}

// LogicalBackArrow is the common back-navigation spelling for copy that owns
// its arrow as text rather than as an SVG icon.
func LogicalBackArrow(locale LocaleContext) string {
	return LogicalArrow(locale, false)
}
