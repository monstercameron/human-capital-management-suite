package tokens

import (
	"fmt"
	"sort"
)

// UIPolish005SemanticNames returns the closed semantic colour vocabulary used
// by theme admission. A fresh slice prevents callers from mutating package
// policy while keeping the vocabulary separate from the renderer palette.
func UIPolish005SemanticNames() []string {
	return []string{
		"accent", "accent-text", "background", "border", "danger", "danger-text",
		"focus", "hover", "hover-text", "info", "selection", "selection-text",
		"success", "surface", "text", "text-muted", "warning", "warning-text",
	}
}

// ValidateUIPolish005SemanticTokens validates a tenant's semantic token set,
// including its exact vocabulary and #rrggbb values. It does not decide
// contrast floors; wcag.QualifyTheme performs that relationship check.
func ValidateUIPolish005SemanticTokens(colors map[string]string) error {
	names := UIPolish005SemanticNames()
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		allowed[name] = struct{}{}
		value, ok := colors[name]
		if !ok {
			return fmt.Errorf("missing semantic token %q", name)
		}
		if _, err := ContrastRatio(value, "#000000"); err != nil {
			return fmt.Errorf("semantic token %q: %w", name, err)
		}
	}
	for name := range colors {
		if _, ok := allowed[name]; !ok {
			return fmt.Errorf("unrecognized semantic token %q", name)
		}
	}
	return nil
}

// SortedUIPolish005SemanticNames returns a stable copy suitable for evidence.
func SortedUIPolish005SemanticNames() []string {
	result := append([]string(nil), UIPolish005SemanticNames()...)
	sort.Strings(result)
	return result
}
