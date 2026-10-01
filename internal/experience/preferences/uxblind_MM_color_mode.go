package preferences

import "strings"

// NormalizePersonalColorMode admits only the personal overrides. Empty means
// that the principal inherits the organization's color mode.
func NormalizePersonalColorMode(value string) string {
	switch value = strings.ToLower(strings.TrimSpace(value)); value {
	case "light", "dark":
		return value
	default:
		return ""
	}
}
