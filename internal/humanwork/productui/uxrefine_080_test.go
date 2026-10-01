package productui

import "testing"

func TestUXRefine080LegacyAppearanceRouteLookup(t *testing.T) {
	for _, route := range []string{
		"/workspace/app/appearance",
		"/workspace/app/appearance/",
		"/workspace/app/appearance?section=brand",
		"/workspace/app/appearance/?section=brand",
	} {
		t.Run(route, func(t *testing.T) {
			page, _, _, ok := RouteProfiles(route)
			if !ok || page != PageAppearance {
				t.Fatalf("RouteProfiles(%q) = %q, %v; want PageAppearance, true", route, page, ok)
			}
			module, ok := LookupRouteModule(route)
			if !ok || module.Definition.ID != PageAppearance {
				t.Fatalf("LookupRouteModule(%q) = %q, %v; want PageAppearance, true", route, module.Definition.ID, ok)
			}
		})
	}
}

func TestUXRefine080NavigationKeepsCanonicalAppearanceRoute(t *testing.T) {
	page, ok := LookupPage(PageAppearance)
	if !ok {
		t.Fatal("PageAppearance is not registered")
	}
	if page.Route != "/workspace/app/admin/appearance" || Path(PageAppearance) != page.Route {
		t.Fatalf("appearance navigation route = %q / %q; want canonical admin route", page.Route, Path(PageAppearance))
	}
}
