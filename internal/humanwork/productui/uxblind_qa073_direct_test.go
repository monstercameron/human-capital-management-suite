package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_073_DirectInsightsRoute(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		t.Run(locale, func(t *testing.T) {
			view := employeePageTestView(PageInsights)
			view.Locale = ResolveProductLocale(locale)
			view.JourneyPopulation = &JourneyPopulation{}
			view.Work = nil
			view.People = nil

			body, err := ui.RenderToString(BuildPageContent(view))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				view.Locale.Text("shell.page_unavailable"),
				view.Locale.Text("shell.page_recovery"),
			} {
				if !strings.Contains(body, want) {
					t.Errorf("direct Insights route omitted localized recovery text %q: %s", want, body)
				}
			}
			for _, hidden := range []string{
				view.Locale.Text("insights.no_data_title"),
				"access scope",
				"does not establish an organization-wide count",
			} {
				if strings.Contains(body, hidden) {
					t.Errorf("empty direct Insights route exposed %q: %s", hidden, body)
				}
			}
			doc, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{view.Locale.Text("shell.page_unavailable"), view.Locale.Text("shell.page_recovery")} {
				if !strings.Contains(doc, want) {
					t.Errorf("SSR Insights route omitted localized recovery text %q", want)
				}
			}

			filtered := contentAwareNavigation(view)
			if navigationContainsPage(filtered.Navigation, PageInsights) || navigationContainsPage(filtered.NavigationSupport, PageInsights) {
				t.Fatal("empty direct Insights route remained in navigation")
			}
		})
	}
}

func navigationContainsPage(items []NavItem, page PageID) bool {
	for _, item := range items {
		if item.Page == page || navigationContainsPage(item.Children, page) {
			return true
		}
	}
	return false
}

func TestTodo_UXBLIND_073_InsightsRetainsAuthorizedContent(t *testing.T) {
	view := testView(PageInsights)
	view.JourneyPopulation = &JourneyPopulation{Total: 1, Active: 1}

	body, err := ui.RenderToString(BuildPageContent(view))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, view.Locale.Text("insights.visible_label")) {
		t.Fatal("authorized Insights metrics were hidden despite a non-empty journey summary")
	}
	if strings.Contains(body, view.Locale.Text("shell.page_unavailable")) {
		t.Fatal("authorized Insights route rendered the unavailable state")
	}
}
