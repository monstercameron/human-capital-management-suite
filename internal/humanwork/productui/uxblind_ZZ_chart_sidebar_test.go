package productui

import (
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UXBLIND_112(t *testing.T) {
	markup, err := ui.RenderToString(ui.CreateElement(BarChart, BarChartProps{
		Title: "Headcount by unit",
		Facts: []FactProps{
			{Label: "Business Development", Value: "12"},
			{Label: "Commerce City, CO yard", Value: "4"},
			{Label: "People Operations", Value: "2"},
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="workforce-bar-legend"`,
		`aria-label="Headcount by unit legend"`,
		`Business Development: 12`,
		`Commerce City, CO yard: 4`,
		`People Operations: 2`,
		`class="workforce-bar-label" title="Business Development"`,
		`title="Commerce City, CO yard: 4"`,
		`class="bar-fill workforce-bar-fill workforce-bar-fill-0"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("bar chart missing %q: %s", want, markup)
		}
	}
	sheet := Stylesheet()
	for _, want := range []string{
		`.insights-breakdown .workforce-bar-row{grid-template-columns:176px minmax(0px,1fr) 40px;`,
		`.workforce-bar-label{display:block;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;}`,
		`.bar-fill.workforce-bar-fill-0,.workforce-bar-swatch-0{background:var(--accent);}`,
	} {
		if !strings.Contains(sheet, want) {
			t.Fatalf("bar chart stylesheet missing %q: %s", want, sheet)
		}
	}
}

func TestTodo_UXBLIND_112_Browser(t *testing.T) {
	doc, err := Render(testView(PageInsights))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`insights-workforce`, `workforce-bar-legend`, `workforce-bar-label`, `title="Strategy: 1"`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("rendered Insights chart missing %q: %s", want, doc)
		}
	}
}

func TestTodo_UXBLIND_113(t *testing.T) {
	props := navigationSidebarProps(testView(PageInsights))
	if got := navigationCurrentKey(props); got != string(PageInsights) {
		t.Fatalf("active navigation key = %q, want %q", got, PageInsights)
	}
	markup, err := ui.RenderToString(ui.CreateElement(NavigationSidebar, props))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `data-hcm-nav-current="true"`) {
		t.Fatalf("active navigation item is missing the scroll marker: %s", markup)
	}
}

func TestTodo_UXBLIND_113_Browser(t *testing.T) {
	doc, err := Render(testView(PageAdmin))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `id="primary-nav"`) || !strings.Contains(doc, `data-hcm-nav-current="true"`) {
		t.Fatalf("rendered sidebar does not expose the active item scroll target: %s", doc)
	}
	if !strings.Contains(string(readProductUISource(t, "uxblind_tt_nav_scroll_wasm.go")), `"block": "nearest"`) {
		t.Fatal("sidebar navigation scroll does not use nearest block alignment")
	}
}

func readProductUISource(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
