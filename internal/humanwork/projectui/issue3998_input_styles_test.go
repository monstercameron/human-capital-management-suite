package projectui

import (
	"strings"
	"testing"
)

// The input dialog holds at phone width and right to left: logical properties
// only, wrapping text, comfortable touch targets, a visible focus ring.
func TestIssue3998_InputStylesAreLogicalTouchSizedAndWrap(t *testing.T) {
	var rules []string
	for _, line := range strings.Split(projectUIShareStyles, "\n") {
		if strings.Contains(line, "projectui-input-") {
			rules = append(rules, line)
		}
	}
	css := strings.Join(rules, "\n")
	if css == "" {
		t.Fatal("no input styles")
	}
	for _, physical := range []string{"margin-left", "margin-right", "padding-left", "padding-right", "text-align:left", "text-align:right", "float:", " left:", " right:"} {
		if strings.Contains(css, physical) {
			t.Errorf("physical property %q breaks right-to-left", physical)
		}
	}
	for _, want := range []string{"[hidden]{display:none}", "min-block-size:2.75rem", "focus-visible", "overflow-wrap:anywhere", "white-space:pre-wrap"} {
		if !strings.Contains(css, want) {
			t.Errorf("missing %q", want)
		}
	}
}
