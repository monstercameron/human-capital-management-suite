package productui

import (
	"fmt"
	"strings"
	"unicode"
)

// ValidateContentSafety inspects every content-tier widget
// binding's Value and DisplayValue for the stored-content
// injection the editor deliberately leaves to publication: raw
// markup, script-bearing URL schemes, and encoded entities.
// Governed and external tiers stay outside the gate — typed
// contracts and sandboxed embeds are not sanitized summaries —
// and bindings naming an unregistered widget are left to the
// widgets step, which already refuses them. Findings accumulate
// in composition order, value before display value; reasons
// stay nil on success. The check is pure and idempotent: the
// same draft always reports the same reasons.
func ValidateContentSafety(draft PageDraft, registry WidgetRegistry) WidgetVerdict {
	tiers := make(map[string]string, len(registry.Widgets))
	for _, widget := range registry.Widgets {
		tiers[widget.ID] = widget.Tier
	}
	var reasons []string
	for _, binding := range draft.Composition.Widgets {
		tier, ok := tiers[binding.WidgetType]
		if !ok || tier != WidgetTierContent {
			continue
		}
		for _, field := range []struct{ name, value string }{
			{"value", binding.Value},
			{"display value", binding.DisplayValue},
		} {
			if containsRawMarkup(field.value) {
				reasons = append(reasons, "content "+field.name+" carries raw markup")
				continue
			}
			if scheme := unsafeContentScheme(field.value); scheme != "" {
				reasons = append(reasons, fmt.Sprintf("content %s uses unsafe URL scheme %q", field.name, scheme))
				continue
			}
			if containsEncodedEntity(field.value) {
				reasons = append(reasons, "content "+field.name+" carries encoded entity")
			}
		}
	}
	if len(reasons) > 0 {
		return WidgetVerdict{Compatible: false, Reasons: reasons}
	}
	return WidgetVerdict{Compatible: true}
}

// unsafeContentScheme reports the script-bearing URL scheme a
// content value opens with — javascript:, vbscript: or data: —
// past the leading whitespace and control characters browsers
// ignore, matched case-insensitively. Ordinary links, bare text
// and empty values report none.
func unsafeContentScheme(value string) string {
	trimmed := strings.TrimLeftFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	})
	lowered := strings.ToLower(trimmed)
	for _, scheme := range []string{"javascript:", "vbscript:", "data:"} {
		if strings.HasPrefix(lowered, scheme) {
			return scheme
		}
	}
	return ""
}

// containsEncodedEntity sniffs HTML character references —
// &name;, &#123; and &#x1F; — in a value the renderer escapes
// at render time, where an entity would decode into markup or a
// disguised character instead of showing literally. A bare
// ampersand with no semicolon form passes.
func containsEncodedEntity(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] != '&' {
			continue
		}
		rest := value[i+1:]
		if strings.HasPrefix(rest, "#") {
			digits := rest[1:]
			hexadecimal := false
			if strings.HasPrefix(digits, "x") || strings.HasPrefix(digits, "X") {
				hexadecimal = true
				digits = digits[1:]
			}
			j := 0
			for j < len(digits) && isEntityDigit(digits[j], hexadecimal) {
				j++
			}
			if j > 0 && j < len(digits) && digits[j] == ';' {
				return true
			}
			continue
		}
		j := 0
		for j < len(rest) && isASCIILetter(rest[j]) {
			j++
		}
		if j > 0 && j < len(rest) && rest[j] == ';' {
			return true
		}
	}
	return false
}

// isASCIILetter reports ASCII letters for named references.
func isASCIILetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// isEntityDigit reports reference digits: decimal, or hex when
// hexadecimal.
func isEntityDigit(c byte, hexadecimal bool) bool {
	if c >= '0' && c <= '9' {
		return true
	}
	return hexadecimal && (c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F')
}
