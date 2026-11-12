package productui

import "testing"

// TestIssue3595LauncherDestinationKeepsURLLocale pins that a projected
// navigation destination opened from the action launcher keeps the active
// resolved URL locale, the same way the sidebar link does.
func TestIssue3595LauncherDestinationKeepsURLLocale(t *testing.T) {
	for _, tc := range []struct{ locale, want string }{
		{"en-US", "/workspace/app/projects?locale=en-US"},
		{"ar", "/workspace/app/projects?locale=ar"},
	} {
		view := testView(PageHome)
		view.Locale = ResolveProductLocale(tc.locale)
		view.Navigation = []NavItem{{Page: PageProjects, Label: "Projects", Href: "/workspace/app/projects"}}
		var found *ActionLauncherItem
		items := actionLauncherProps(view).Items
		for index := range items {
			if items[index].Kind == ActionLauncherDestination && items[index].Page == PageProjects {
				found = &items[index]
			}
		}
		if found == nil {
			t.Fatalf("%s: launcher has no Projects destination: %+v", tc.locale, items)
		}
		if found.Href != tc.want {
			t.Fatalf("%s: launcher Projects href = %q, want %q", tc.locale, found.Href, tc.want)
		}
	}
}
