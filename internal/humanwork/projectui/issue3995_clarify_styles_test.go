package projectui

import (
	"strings"
	"testing"
)

// The clarification slot and dialog hold at phone width and right to left:
// logical properties only, wrapping text, 44px targets on touch, a visible
// focus ring and no motion when the reader asks for none.
func TestIssue3995_ClarifyStylesAreLogicalTouchSizedAndCalm(t *testing.T) {
	var rules []string
	for _, line := range strings.Split(projectUIShareStyles, "\n") {
		if strings.Contains(line, "clarify") {
			rules = append(rules, line)
		}
	}
	css := strings.Join(rules, "\n")
	if css == "" {
		t.Fatal("no clarification styles")
	}
	for _, physical := range []string{"margin-left", "margin-right", "padding-left", "padding-right", "text-align:left", "text-align:right", "float:", " left:", " right:"} {
		if strings.Contains(css, physical) {
			t.Errorf("physical property %q breaks right-to-left", physical)
		}
	}
	for _, want := range []string{"[hidden]{display:none}", "min-block-size:2.75rem", "focus-visible", "prefers-reduced-motion:reduce", "overflow-wrap:anywhere", "min(32rem,calc(100vw - 2rem))"} {
		if !strings.Contains(css, want) {
			t.Errorf("missing %q", want)
		}
	}
}
