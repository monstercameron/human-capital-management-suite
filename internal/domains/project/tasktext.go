package project

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	MaxTaskTitleRunes       = 200
	MaxTaskDescriptionRunes = 20_000
)

// ValidateTaskText enforces the canonical task text bounds at the domain
// boundary. Markup is data here; renderers must escape it or use the approved
// rich-text sanitizer before emitting HTML.
func ValidateTaskText(title, description string) bool {
	return validTaskText(title, MaxTaskTitleRunes, false) &&
		strings.TrimSpace(title) != "" &&
		validTaskText(description, MaxTaskDescriptionRunes, true)
}

func validTaskText(value string, maxRunes int, allowWhitespaceControls bool) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRunes {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && !(allowWhitespaceControls && (r == '\n' || r == '\r' || r == '\t')) {
			return false
		}
	}
	return true
}
