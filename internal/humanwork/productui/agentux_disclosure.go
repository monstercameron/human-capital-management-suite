package productui

import "github.com/monstercameron/human-capital-management-suite/internal/experience/localize"

// agentDisclosureChevron is the glyph a collapsed disclosure chevron shows.
// It points forward: right in left-to-right locales, left in right-to-left
// ones. The open state turns it down with a rotate in the stylesheet, so
// only the closed glyph needs the direction.
func agentDisclosureChevron(locale LocaleContext) string {
	if locale.Direction == localize.RTL {
		return "‹"
	}
	return "›"
}
