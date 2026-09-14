package productui

import (
	"strings"
	"testing"
)

func TestTodo_UIPOLISH_001_BrandFallbackWrapsWithoutClipping(t *testing.T) {
	css := Stylesheet()
	selector := ".wordmark-label{"
	start := strings.Index(css, selector)
	if start < 0 {
		t.Fatal("brand fallback label has no production style")
	}
	for start >= 0 {
		end := strings.IndexByte(css[start:], '}')
		if end < 0 {
			t.Fatal("brand fallback label rule is malformed")
		}
		rule := css[start : start+end]
		if strings.Contains(rule, "max-width:100%") {
			for _, forbidden := range []string{"overflow:hidden", "text-overflow:ellipsis", "white-space:nowrap"} {
				if strings.Contains(rule, forbidden) {
					t.Errorf("brand fallback label still clips with %q: %s", forbidden, rule)
				}
			}
			break
		}
		next := strings.Index(css[start+len(selector):], selector)
		if next < 0 {
			t.Fatal("brand fallback label has no wrapping rule")
		}
		start += len(selector) + next
	}
	for _, fragment := range []string{
		"max-width:100%",
		"overflow-wrap:anywhere",
		"text-overflow:clip",
		"white-space:normal",
		"line-height:1.15",
	} {
		if !strings.Contains(css, fragment) {
			t.Errorf("brand fallback label rule missing %q", fragment)
		}
	}
}
