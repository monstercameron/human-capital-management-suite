package workspace

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestShellFix_ExplicitExpandedNavigationWinsTabletDefault(t *testing.T) {
	config := JourneyConfig{Tenant: "harborcare-demo", Roles: []string{"comp_admin"}}
	for _, test := range []struct {
		nav       string
		wantClass string
	}{
		{nav: ""},
		{nav: "expanded", wantClass: "nav-expanded"},
		{nav: "collapsed", wantClass: "nav-collapsed"},
	} {
		doc, err := productShellDocumentForRouteState(config, true, productui.ResolveProductLocale("en-US"), productui.PageAgentOperations, "", test.nav)
		if err != nil {
			t.Fatal(err)
		}
		start := strings.Index(doc, `class="app-shell`)
		if start < 0 {
			t.Fatalf("nav=%q has no app shell class", test.nav)
		}
		end := strings.Index(doc[start+len(`class="`):], `"`)
		if end < 0 {
			t.Fatalf("nav=%q has an unterminated app shell class", test.nav)
		}
		classes := strings.Fields(doc[start+len(`class="`) : start+len(`class="`)+end])
		has := func(want string) bool {
			for _, class := range classes {
				if class == want {
					return true
				}
			}
			return false
		}
		if !has("app-shell") || (test.wantClass != "" && !has(test.wantClass)) || (test.wantClass != "nav-expanded" && has("nav-expanded")) || (test.wantClass != "nav-collapsed" && has("nav-collapsed")) {
			t.Fatalf("nav=%q shell classes = %v, want app-shell plus %q only", test.nav, classes, test.wantClass)
		}
	}
}
