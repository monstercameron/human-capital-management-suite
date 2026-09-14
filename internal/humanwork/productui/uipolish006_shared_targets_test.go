package productui

import (
	"regexp"
	"strings"
	"testing"
)

// TestTodo_UIPOLISH_006_SharedTargets covers the security, recovery and shell
// actions that are not represented by the generic .button primitive. These
// controls are real links/buttons, while the similarly named status and
// capability classes are informational and intentionally excluded.
func TestTodo_UIPOLISH_006_SharedTargets(t *testing.T) {
	css := Stylesheet()
	for _, selector := range []string{
		".utility-drawer-trigger", ".utility-drawer-close", ".utility-drawer-item a",
		".session-warning-reauth", ".session-warning-dismiss", ".delegation-selector-summary",
		"a.step-up-challenge", ".step-up-dismiss", "a.break-glass-activate",
		".break-glass-dismiss", "a.policy-simulation-exit", "a.signed-out-signin",
	} {
		if !regexp.MustCompile(regexp.QuoteMeta(selector) + `\{[^}]*min-height:44px`).MatchString(css) {
			t.Errorf("%s has no 44px interactive target", selector)
		}
	}
	if !strings.Contains(css, `@media (max-width:760px){.viewer-profile-link{height:44px;min-height:44px;width:44px;}`) {
		t.Error("mobile viewer profile link has no 44px target")
	}
}
