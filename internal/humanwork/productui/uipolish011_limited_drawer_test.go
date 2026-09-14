package productui

import (
	"regexp"
	"strings"
	"testing"
)

func TestTodo_UIPOLISH_011_Regression_LimitedDrawerTransitionIsImmediate(t *testing.T) {
	css := Stylesheet()
	selector := `:root[data-hcm-motion-preference="limited"] .sidebar,:root[data-hcm-motion-preference="limited"] .sidebar.collapsed`
	if !regexp.MustCompile(regexp.QuoteMeta(selector) + `\{[^}]*transition:none!important`).MatchString(css) {
		t.Fatal("production limited-motion drawer still travels")
	}
	if !strings.Contains(css, "inset-inline-start:-336px") || !strings.Contains(css, "inset-inline-start:0!important") {
		t.Fatal("limited motion changed the drawer's open/closed state contract")
	}
}
