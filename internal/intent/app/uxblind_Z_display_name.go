package app

import "strings"

// journeyDisplayName is the viewer-safe name stored on promotion journeys.
// The preferred name stays primary, while the legal family name disambiguates
// workers whose preferred names are equal. Empty facts fall back without
// fabricating a label.
func journeyDisplayName(preferred, legal string) string {
	preferred = strings.TrimSpace(preferred)
	legal = strings.TrimSpace(legal)
	if preferred == "" {
		return legal
	}
	if legal == "" {
		return preferred
	}
	legalParts := strings.Fields(legal)
	preferredParts := strings.Fields(preferred)
	if len(legalParts) == 0 || len(preferredParts) == 0 {
		return preferred
	}
	family := legalParts[len(legalParts)-1]
	if strings.EqualFold(preferredParts[len(preferredParts)-1], family) {
		return preferred
	}
	return preferred + " " + family
}
