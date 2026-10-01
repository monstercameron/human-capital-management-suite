package workspace

import (
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

func TestTodo_UXBLIND_124(t *testing.T) {
	config := uxblind124Config()
	locale := productui.ResolveProductLocale("en-US")
	favorites := []productui.PageID{productui.PagePeople, productui.PageInsights}
	groups := map[productui.PageID]bool{productui.PageWork: false, productui.PageAdmin: true}

	for _, page := range []productui.PageID{productui.PageClock, productui.PageAgents} {
		t.Run(string(page), func(t *testing.T) {
			loading, err := productShellDocumentForRouteStateWithPreferencesAndNavigation(
				config, true, locale, page, "", "", productui.DefaultCustomerTheme(),
				productui.DefaultAccessibilityPreferences(), productStylesheet(), favorites, groups,
			)
			if err != nil {
				t.Fatalf("render loading shell: %v", err)
			}

			hydrated, err := ui.RenderToString(productui.Build(uxblind124View(config, page, locale, favorites, groups)))
			if err != nil {
				t.Fatalf("render hydrated shell: %v", err)
			}
			for _, tag := range []string{"header", "aside", "footer"} {
				if got, want := uxblindAB114Element(loading, tag), uxblindAB114Element(hydrated, tag); got != want {
					t.Fatalf("%s differs from hydrated navigation model\nloading: %s\nhydrated: %s", tag, got, want)
				}
			}
			assertUXBlind124Navigation(t, uxblind124View(config, page, locale, favorites, groups))
		})
	}
}

// TestTodo_UXBLIND_124_Browser is the component-level browser contract. The
// loopback browser harness is covered by the shell tests; this test pins the
// rendered route states that a browser receives before hydration.
func TestTodo_UXBLIND_124_Browser(t *testing.T) {
	config := uxblind124Config()
	locale := productui.ResolveProductLocale("en-US")
	favorites := []productui.PageID{productui.PagePeople, productui.PageInsights}
	groups := map[productui.PageID]bool{productui.PageWork: false, productui.PageAdmin: true}

	for _, page := range []productui.PageID{productui.PageClock, productui.PageAgents} {
		t.Run(string(page), func(t *testing.T) {
			view := uxblind124View(config, page, locale, favorites, groups)
			assertUXBlind124Navigation(t, view)
			markup, err := ui.RenderToString(productui.Build(view))
			if err != nil {
				t.Fatalf("render %s: %v", page, err)
			}
			if markup == "" {
				t.Fatalf("render %s returned empty markup", page)
			}
		})
	}
}

func uxblind124Config() JourneyConfig {
	return JourneyConfig{
		Tenant: shellTenant, Subject: shellSubject,
		Roles:           []string{productui.RoleHCMAdmin, "comp_admin"},
		PagePermissions: roleaccess.DefaultPagePermissions(),
		Agents:          &AgentsConfig{Enabled: true, Service: agentServiceAvailable},
		Clock:           &ClockConfig{Enabled: true},
	}
}

func uxblind124View(config JourneyConfig, page productui.PageID, locale productui.LocaleContext, favorites []productui.PageID, groups map[productui.PageID]bool) productui.View {
	view := productui.NewView(page, productui.DisplayLabel(config.Tenant), productui.DisplayLabel(config.Subject), "")
	view.FavoritePages = append([]productui.PageID(nil), favorites...)
	view.NavigationGroupOpen = groups
	view = productui.ApplyRoleVisibility(view, config.Roles)
	if len(config.PagePermissions) > 0 {
		view = productui.ApplyPagePermissions(view, productPagePermissions(config.PagePermissions))
	}
	if config.Agents != nil {
		view = productui.ApplyAgentsAvailability(view, ProductAgentsAvailability(config.Agents))
	}
	if config.Clock != nil {
		view = productui.ApplyClockAvailability(view, ProductClockAvailability(config.Clock))
	}
	return productui.ApplyLocale(view, locale)
}

func assertUXBlind124Navigation(t *testing.T, view productui.View) {
	t.Helper()
	seen := map[productui.PageID]bool{}
	var walk func([]productui.NavItem)
	walk = func(items []productui.NavItem) {
		for _, item := range items {
			definition, ok := productui.LookupPage(item.Page)
			if !ok {
				t.Errorf("navigation contains unknown page %q", item.Page)
			} else if item.Icon != definition.Icon {
				t.Errorf("navigation icon for %q = %q, want canonical %q", item.Page, item.Icon, definition.Icon)
			}
			// Group headings intentionally share their stable page ID with
			// their Overview child (for example Work and Admin). Duplicate
			// destination leaves, however, are a broken navigation contract.
			if len(item.Children) == 0 {
				if seen[item.Page] {
					t.Errorf("navigation contains duplicate destination %q", item.Page)
				}
				seen[item.Page] = true
			}
			walk(item.Children)
		}
	}
	walk(view.Navigation)
	for _, page := range []productui.PageID{productui.PageClock, productui.PageAgents} {
		if !seen[page] {
			t.Errorf("navigation omitted %q", page)
		}
	}
}
