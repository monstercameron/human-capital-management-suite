package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// TestTodo_UXBLIND_027 walks the registered page catalogue and checks the
// same localized page name at each shell boundary: navigation, heading, and
// breadcrumb. The action-launcher destination projection is also derived from
// the navigation item, so exercising that item keeps the test on the registry
// contract rather than a list of hand-maintained page names.
func TestTodo_UXBLIND_027_KK(t *testing.T) {
	for _, code := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(code)
		for _, definition := range registeredPages() {
			name := strings.TrimSpace(locale.Text(definition.LabelKey))
			if name == "" {
				t.Fatalf("%s %s has no registered localized name", code, definition.ID)
			}
			if title := strings.TrimSpace(locale.Text(definition.TitleKey)); title != name {
				t.Fatalf("%s %s has sidebar name %q but title %q", code, definition.ID, name, title)
			}

			navigation := navigationItemFromDefinition(definition, locale)
			if navigation.Label != name || navigation.LabelKey != definition.LabelKey {
				t.Fatalf("%s %s navigation identity = %#v, want %q from %q", code, definition.ID, navigation, name, definition.LabelKey)
			}
			view := View{Page: definition.ID, Locale: locale}
			if identity := ResolvePageIdentity(view); identity.Title != name {
				t.Fatalf("%s %s heading = %q, want %q", code, definition.ID, identity.Title, name)
			}
			crumbs := ResolveBreadcrumbs(view)
			if len(crumbs) == 0 || !crumbs[len(crumbs)-1].Current || crumbs[len(crumbs)-1].Label != name {
				t.Fatalf("%s %s breadcrumb = %#v, want current %q", code, definition.ID, crumbs, name)
			}
		}
	}
}

func TestTodo_UXBLIND_027_KK_Browser(t *testing.T) {
	view := View{Page: PageWorkflowDesigner, Locale: ResolveProductLocale("en-US")}
	item, ok := navigationItemForPage(PageWorkflowDesigner, view.Locale)
	if !ok {
		t.Fatal("Workflow Designer is not registered for navigation")
	}
	markup, err := ui.RenderToString(NavigationItem(navigationLeafProps(view, item, true)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Workflow Designer", `aria-label="Workflow Designer"`, `title="Workflow Designer"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("registered page name is missing from browser navigation markup: %q\n%s", want, markup)
		}
	}
}

func TestTodo_UXBLIND_027_KK_I18n(t *testing.T) {
	for _, code := range []string{"en-US", "de-DE", "ar"} {
		locale := ResolveProductLocale(code)
		for _, definition := range registeredPages() {
			label, title := locale.Text(definition.LabelKey), locale.Text(definition.TitleKey)
			if label == title {
				continue
			}
			t.Errorf("%s %s diverges between label %q and title %q", code, definition.ID, label, title)
		}
	}
}

func TestTodo_UXBLIND_040_KK(t *testing.T) {
	css := Stylesheet()
	for selector, properties := range map[string][]string{
		`.access-role-identity code`:                                  {"overflow-wrap:normal", "word-break:normal", "white-space:nowrap", "overflow:hidden", "text-overflow:ellipsis"},
		`.organization-visibility-unit>span,.organization-scope-unit`: {"overflow-wrap:normal", "word-break:normal", "white-space:nowrap", "overflow:hidden", "text-overflow:ellipsis"},
		`.profile-fact dd.profile-fact-code`:                          {"overflow-wrap:normal", "word-break:normal", "white-space:nowrap", "overflow:hidden", "text-overflow:ellipsis"},
	} {
		start := strings.Index(css, selector+"{")
		if start < 0 {
			t.Fatalf("identifier style is missing selector %q", selector)
		}
		end := strings.IndexByte(css[start:], '}')
		if end < 0 {
			t.Fatalf("identifier style for %q is unterminated", selector)
		}
		rule := css[start : start+end]
		for _, property := range properties {
			if !strings.Contains(rule, property) {
				t.Errorf("identifier style %q is missing %q: %s", selector, property, rule)
			}
		}
	}
	if strings.Contains(css, `.access-role-identity code{`) && strings.Contains(css, `.access-role-identity code{min-width:0;max-inline-size:100%;overflow-wrap:anywhere`) {
		t.Fatal("role identifiers still permit arbitrary mid-word wrapping")
	}
}

func TestTodo_UXBLIND_040_KK_Browser(t *testing.T) {
	css := Stylesheet()
	// Both requested desktop widths select the same min-width desktop rules;
	// the contract is intentionally expressed as one range rather than a
	// width-specific page hack.
	if !strings.Contains(css, `@media (min-width:761px)`) {
		t.Fatal("desktop label rules do not cover 800px and 1280px viewports")
	}
	for _, selector := range []string{".access-role-identity code", ".organization-visibility-unit>span", ".profile-fact dd.profile-fact-code"} {
		if !strings.Contains(css, selector) {
			t.Fatalf("desktop identifier contract is missing %s", selector)
		}
	}
}

func TestTodo_UXBLIND_091_KK(t *testing.T) {
	view := View{Page: PageWorkflowDesigner, Locale: ResolveProductLocale("en-US")}
	item, ok := navigationItemForPage(PageWorkflowDesigner, view.Locale)
	if !ok || item.Label != "Workflow Designer" {
		t.Fatalf("registered Workflow Designer navigation item = %#v", item)
	}
	props := navigationLeafProps(view, item, true)
	markup, err := ui.RenderToString(NavigationItem(props))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, "Workflow Designer") || !strings.Contains(markup, "favorite") {
		t.Fatalf("sidebar lost the complete label/favorite control: %s", markup)
	}
	css := Stylesheet()
	if strings.Contains(css, `.primary-nav>ul>.nav-entry>.nav-link:not(.has-search-detail) .nav-copy>.nav-label{white-space:nowrap`) ||
		strings.Contains(css, `.primary-nav>ul>.nav-entry>.nav-link:not(.has-search-detail) .nav-copy>.nav-label{overflow-wrap:anywhere`) {
		t.Fatal("top-level registered labels are still truncated or split mid-word")
	}
}

func TestTodo_UXBLIND_091_KK_Regression(t *testing.T) {
	css := Stylesheet()
	for _, selector := range []string{
		`:where(.sidebar:not(.collapsed)) .primary-nav .nav-group>.nav-group-summary>.nav-label`,
		`.primary-nav>ul>.nav-entry>.nav-link:not(.has-search-detail) .nav-copy>.nav-label`,
	} {
		if !strings.Contains(css, selector) {
			t.Fatalf("sidebar wrapping rule disappeared for %s", selector)
		}
	}
}
