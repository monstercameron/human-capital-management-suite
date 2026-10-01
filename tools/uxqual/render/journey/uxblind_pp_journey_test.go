package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func renderPP(t *testing.T, node ui.Node) string {
	t.Helper()
	markup, err := ui.RenderToString(node)
	if err != nil {
		t.Fatal(err)
	}
	return markup
}

func TestTodo_UXBLIND_094(t *testing.T) {
	link := DetailView{BackLink: NavLink{Href: "/people/ana", Label: "← Back to Ana's profile"}}
	en := renderPP(t, detailNavigationLocale("en-US", link))
	ar := renderPP(t, detailNavigationLocale("ar", link))
	if strings.Count(en, "←") != 1 || strings.Contains(en, "← ←") {
		t.Fatalf("English back link does not have one back arrow: %s", en)
	}
	if strings.Count(ar, "→") != 1 || strings.Contains(ar, "→ →") {
		t.Fatalf("Arabic back link does not have one direction-correct arrow: %s", ar)
	}
}

func TestTodo_UXBLIND_094_Browser(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup := renderPP(t, detailNavigationLocale(locale, DetailView{BackLink: NavLink{Href: "/people/ana", Label: "→ ← Back to Ana"}}))
		if strings.Count(markup, "←")+strings.Count(markup, "→") != 1 {
			t.Fatalf("%s back link has duplicate directional glyphs: %s", locale, markup)
		}
	}
}

func TestTodo_UXBLIND_095(t *testing.T) {
	card := JourneyCard{
		WorkerName:     "Ana Reyes",
		Headline:       "Journeyman Carpenter (C3) → Foreman (C4)",
		PlacementCodes: "IR-JCP → IR-FMN",
		PayLine:        "USD 34.50 per hour → USD 40.00 per hour (+15.9%)",
		BusinessReason: "Ana has run the framing crew on two jobs.",
		StageLabel:     "Awaiting approval",
		StageTone:      toneWarning,
	}
	markup := renderPP(t, journeyCardLocale("en-US", card))
	pay := strings.Index(markup, `class="jn-journey-pay"`)
	reason := strings.Index(markup, `class="jn-journey-reason"`)
	placement := strings.Index(markup, `class="jn-journey-headline"`)
	if pay < 0 || reason < 0 || placement < 0 || !(pay < reason && reason < placement) {
		t.Fatalf("card facts are not separated in reading order: %s", markup)
	}
	if strings.Contains(markup, "[IR-") || strings.Contains(markup, "Business reason:") && strings.Contains(markup[strings.Index(markup, `class="jn-journey-pay"`):reason], "Business reason") {
		t.Fatalf("card still embeds codes or the reason in the headline pay line: %s", markup)
	}
}

func TestTodo_UXBLIND_095_Browser(t *testing.T) {
	markup := renderPP(t, heroSectionLocale("en-US", JourneyCard{
		WorkerName: "Ana Reyes", Headline: "Journeyman Carpenter (C3) → Foreman (C4)", PlacementCodes: "IR-JCP → IR-FMN",
		PayLine: "USD 34.50 per hour → USD 40.00 per hour (+15.9%)", BusinessReason: "Ana has run the framing crew on two jobs.",
	}, false))
	if !strings.Contains(markup, `class="jn-hero-pay"`) || !strings.Contains(markup, `class="jn-journey-reason"`) || !strings.Contains(markup, "jn-journey-codes") {
		t.Fatalf("hero did not render the separated pay, reason and code facts: %s", markup)
	}
	if strings.Contains(markup, "[IR-") {
		t.Fatalf("hero rendered a bracketed placement code: %s", markup)
	}
}

func TestTodo_UXBLIND_096(t *testing.T) {
	filter := &JourneyFilterView{Label: "Find requests", Total: 1, ToggleLabel: "Filters", Fields: []Field{
		{ID: "journey-filter-q", Kind: fieldKindText, Label: "Search"},
		{ID: "journey-filter-status", Kind: fieldKindSelect, Label: "Status"},
		{ID: "journey-filter-sort", Name: "journey-filter-sort", Kind: fieldKindSelect, Label: "Order", Options: []Option{{Value: "recent", Label: "Most recently updated"}}},
	}}
	markup := renderPP(t, journeysSection("en-US", ListView{Filter: filter, Journeys: []JourneyCard{{WorkerName: "Ana"}}}))
	for _, want := range []string{`data-collapsible="true"`, `data-open="false"`, "Filters", `name="journey-filter-sort"`} {
		if !strings.Contains(markup, want) {
			t.Fatalf("short-list filter missing %q: %s", want, markup)
		}
	}
}

func TestTodo_UXBLIND_096_Browser(t *testing.T) {
	sheet := Stylesheet()
	if !strings.Contains(sheet, `data-collapsible="true"`) || !strings.Contains(sheet, `data-open="false"`) {
		t.Fatalf("short-list panel is not conditionally collapsed at desktop widths")
	}
	if !strings.Contains(sheet, `.jn-grid:has(> .jn-griditem:only-child)`) || !strings.Contains(sheet, `grid-template-columns:1fr`) {
		t.Fatalf("a single journey card does not use the available width")
	}
	if !strings.Contains(sheet, `data-field-id`) && !strings.Contains(sheet, `journey-filter-sort`) {
		t.Fatalf("sort control has no width guard")
	}
}
