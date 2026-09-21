package preferences

import "strings"

// The layout densities a person may choose for themselves. They are the same
// closed vocabulary the organization theme uses, so a personal choice can
// only select a density the product already qualifies (including the 44px
// control-height floor every density keeps).
const (
	DensityCompact     = "compact"
	DensityComfortable = "comfortable"
	DensitySpacious    = "spacious"
)

// NormalizeUserDensity returns value when it names an admitted density and
// "" otherwise. "" means "inherit the organization's density", so a corrupt
// or unknown stored value falls back to the shared default rather than
// selecting undeclared presentation (REV-092-01).
func NormalizeUserDensity(value string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
	case DensityCompact, DensityComfortable, DensitySpacious:
		return normalized
	default:
		return ""
	}
}
