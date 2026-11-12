package workspace

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// #3771: the server's first paint of Chat already has the navigation as its
// rail, so the page does not reflow when the client starts. An explicit
// nav=expanded wins, and another page's first paint is unchanged.
func TestChatDesign_3771_FirstPaintOfChatHasTheNavigationRail(t *testing.T) {
	config := JourneyConfig{Tenant: "harborcare-demo", Roles: []string{"comp_admin"}}
	for _, test := range []struct {
		page      productui.PageID
		nav       string
		wantClass string
	}{
		{page: productui.PageChat, nav: "", wantClass: "nav-collapsed"},
		{page: productui.PageChat, nav: "collapsed", wantClass: "nav-collapsed"},
		{page: productui.PageChat, nav: "expanded", wantClass: "nav-expanded"},
		{page: productui.PageHome, nav: ""},
		{page: productui.PageAgents, nav: ""},
	} {
		doc, err := productShellDocumentForRouteState(config, true, productui.ResolveProductLocale("en-US"), test.page, "", test.nav)
		if err != nil {
			t.Fatal(err)
		}
		start := strings.Index(doc, `class="app-shell`)
		if start < 0 {
			t.Fatalf("%s nav=%q has no app shell class", test.page, test.nav)
		}
		end := strings.Index(doc[start+len(`class="`):], `"`)
		if end < 0 {
			t.Fatalf("%s nav=%q has an unterminated app shell class", test.page, test.nav)
		}
		classes := " " + doc[start+len(`class="`):start+len(`class="`)+end] + " "
		for _, class := range []string{"nav-collapsed", "nav-expanded"} {
			if got, want := strings.Contains(classes, " "+class+" "), class == test.wantClass; got != want {
				t.Errorf("%s nav=%q: %s present=%v, want %v (classes %q)", test.page, test.nav, class, got, want, classes)
			}
		}
	}
}
