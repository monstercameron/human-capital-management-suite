package productui

import "strings"

// UnitDisplayName is an organization unit as a person reads it. The directory
// supplies the unit's own name ("Safety & Quality"); only when it has none is
// the unit code turned into words, which cannot recover an ampersand.
func UnitDisplayName(name, code string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return DisplayLabel(code)
}
