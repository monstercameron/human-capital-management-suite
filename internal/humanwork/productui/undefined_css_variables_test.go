package productui

import (
	"regexp"
	"sort"
	"testing"
)

// TestEveryCSSVariableUsedIsDefined: a var() naming a custom property the
// stylesheet never defines falls back silently -- to its fallback value or to
// nothing -- and ignores the customer theme. --surface-hover (six hover
// fills) and --hcm-color-positive (the positive status glyph) both did.
func TestEveryCSSVariableUsedIsDefined(t *testing.T) {
	css := Stylesheet()
	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`[;{]\s*--([a-zA-Z0-9_-]+)\s*:`).FindAllStringSubmatch(css, -1) {
		defined[m[1]] = true
	}
	missing := map[string]bool{}
	for _, m := range regexp.MustCompile(`var\(--([a-zA-Z0-9_-]+)`).FindAllStringSubmatch(css, -1) {
		if !defined[m[1]] {
			missing[m[1]] = true
		}
	}
	if len(missing) > 0 {
		names := make([]string, 0, len(missing))
		for name := range missing {
			names = append(names, "--"+name)
		}
		sort.Strings(names)
		t.Fatalf("the stylesheet uses custom properties it never defines: %v", names)
	}
}
