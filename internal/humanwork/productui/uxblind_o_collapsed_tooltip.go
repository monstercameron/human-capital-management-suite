package productui

import "strings"

// uxblindOCollapsedNavigationTooltipAttrs keeps the native browser tooltip
// and the stable data hook on every rail item. The aria-label remains on the
// link/summary itself, so the collapsed rail does not trade sighted clarity
// for an inaccessible icon-only control.
func uxblindOCollapsedNavigationTooltipAttrs(label string) map[string]any {
	label = strings.TrimSpace(label)
	if label == "" {
		return nil
	}
	return map[string]any{"title": label, "data-nav-tooltip": label}
}
