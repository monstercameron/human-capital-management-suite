package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

func TestTodo_UXBLIND_106(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(InsightsPage, InsightsPageProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Metrics:   []MetricProps{{Label: "Promotion requests", Value: "2"}},
		Workforce: InsightsWorkforceProps{
			Title: "Workforce snapshot", UnitTitle: "Headcount by unit",
			UnitFacts:       []FactProps{{Label: "Safety Quality", Value: "8"}, {Label: "Warranty Service", Value: "4"}},
			ThroughputLabel: "Throughput", ThroughputValue: "2",
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`insights-workforce`, `class="insights-breakdown"`, `class="bars"`, `class="bar-track"`,
		"Safety &amp; Quality", "Warranty &amp; Service", "Promotion requests",
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("workforce snapshot missing %q: %s", want, markup)
		}
	}
	if findClassToken(root, "bar-fill") == nil {
		t.Fatalf("workforce snapshot has no bar fill element: %s", markup)
	}
}

func TestTodo_UXBLIND_106_Browser(t *testing.T) {
	markup, err := Render(testView(PageInsights))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `insights-workforce`) || !strings.Contains(markup, `class="bars"`) {
		t.Fatalf("rendered Insights surface did not expose the styled workforce snapshot: %s", markup)
	}
	if !strings.Contains(markup, "Promotion requests") {
		t.Fatalf("rendered Insights surface kept the old first-card label: %s", markup)
	}
}

func TestTodo_UXBLIND_107(t *testing.T) {
	items := navigationFor(ResolveProductLocale("en-US"), nil)
	for _, item := range items {
		// UXBLIND-102: the My Work child is named "Your actions", distinct from its group; UXBLIND-107 names the Admin child "Overview".
		if item.Page != PageAdmin {
			continue
		}
		if len(item.Children) == 0 || item.Children[0].Label != "Overview" || item.Children[0].LabelKey != "nav.overview" {
			t.Fatalf("%s overview was not named distinctly: %#v", item.Page, item.Children)
		}
	}
	var workerIDs, roles string
	for _, item := range items {
		for _, child := range item.Children {
			switch child.Page {
			case PageWorkerIDs:
				workerIDs = child.Icon
			case PageRoles:
				roles = child.Icon
			}
		}
	}
	if workerIDs == "" || workerIDs == "people" || roles == "" || roles == "admin" || workerIDs == roles {
		t.Fatalf("admin navigation icons are not distinct: worker IDs=%q roles=%q", workerIDs, roles)
	}

	props := navigationSidebarProps(testView(PageInsights))
	if got := navigationCurrentKey(props); got != string(PageInsights) {
		t.Fatalf("current navigation key = %q, want %q", got, PageInsights)
	}
}

func TestTodo_UXBLIND_107_Browser(t *testing.T) {
	markup, err := Render(testView(PageInsights))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `aria-current="page"`) || !strings.Contains(markup, `data-hcm-nav-current="true"`) {
		t.Fatalf("current navigation item lacks the browser scroll marker: %s", markup)
	}
}
