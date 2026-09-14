package productui

import (
	"regexp"
	"strings"
	"testing"
)

func TestTodo_UXAUDIT_009_OpenRoleUsesFullReadableMeasure(t *testing.T) {
	css := Stylesheet()
	for _, pattern := range []string{
		`\.access-role-card\[open\]\{[^}]*grid-column:1 / -1`,
		`\.access-role-identity\{[^}]*flex-wrap:wrap`,
		`\.access-role-card code\{[^}]*overflow-wrap:anywhere`,
	} {
		if !regexp.MustCompile(pattern).MatchString(css) {
			t.Errorf("open role layout is missing %q", pattern)
		}
	}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		label := ResolveProductLocale(locale).Text("roles.definition")
		if label == "" || strings.HasPrefix(label, "roles.") {
			t.Errorf("%s has no role-definition label", locale)
		}
	}
}
