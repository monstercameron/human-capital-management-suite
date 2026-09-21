package productui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestTodo_UIPOLISH_010(t *testing.T) {
	css := Stylesheet()
	for _, selector := range []string{".status-dimension-glyph", ".organization-unit-glyph", ".provenance-item-glyph"} {
		if !strings.Contains(css, selector) {
			t.Errorf("icon contract omits %s", selector)
		}
	}
	for _, declaration := range []string{"display:grid", "min-width:1.15em", "place-items:center"} {
		if !strings.Contains(css, declaration) {
			t.Errorf("icon contract omits %s", declaration)
		}
	}
}

func TestTodo_UIPOLISH_010_Golden(t *testing.T) {
	css := Stylesheet()
	if strings.Contains(css, "url(") {
		t.Fatal("production icon stylesheet must not fetch glyphs")
	}
	if strings.Contains(css, ".status-dimension-glyph[data-hcm-glyphs") {
		t.Fatal("customer glyph preset must not select protected status semantics")
	}
}

func TestTodo_UIPOLISH_010_Browser(t *testing.T) {
	for name, node := range map[string]ui.Node{
		"navigation": navIcon("home"),
		"status":     StatusPresentation(StatusPresentationProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Projection: StatusProjection{Available: true}}),
		"brand":      ui.CreateElement(BrandLogo, BrandLogoProps{Name: "Acme", Mark: "A"}),
	} {
		markup, err := ui.RenderToString(node)
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		if name == "navigation" && (!strings.Contains(markup, `<svg`) || !strings.Contains(markup, `aria-hidden="true"`) || !strings.Contains(markup, `focusable="false"`)) {
			t.Errorf("navigation icon is not decorative inline SVG: %s", markup)
		}
		if name == "status" && !strings.Contains(markup, `class="status-dimension-glyph"`) {
			t.Errorf("status projection omitted its governed glyph: %s", markup)
		}
		if name == "brand" && (!strings.Contains(markup, `data-hcm-brand-name=""`) || !strings.Contains(markup, ">Acme</span>")) {
			t.Errorf("brand fallback lost accessible name: %s", markup)
		}
	}
}

func TestTodo_UIPOLISH_010_Accessibility(t *testing.T) {
	markup, err := ui.RenderToString(navIcon("missing"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `aria-hidden="true"`) || !strings.Contains(markup, `focusable="false"`) {
		t.Fatalf("fallback nav icon is exposed or focusable: %s", markup)
	}
	item := NavigationItemProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Label: "People", Icon: "people", Href: "/workspace/app/people"}
	navMarkup, err := ui.RenderToString(NavigationItem(item))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(navMarkup, ">People</span>") || !strings.Contains(navMarkup, `<svg`) {
		t.Fatalf("navigation icon-only/collapsed projection lacks a text name: %s", navMarkup)
	}
}

func TestTodo_UIPOLISH_010_Security(t *testing.T) {
	css := Stylesheet()
	for _, preset := range []string{"rounded-line", "precision-line", "bold-line"} {
		if strings.Contains(css, `data-hcm-glyphs="`+preset+`" .status-dimension-glyph`) {
			t.Fatalf("glyph preset %q can override protected status glyphs", preset)
		}
	}
	statusMarkup, err := ui.RenderToString(StatusPresentation(StatusPresentationProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}, Projection: StatusProjection{Available: true}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(statusMarkup, `class="status-dimension-glyph"`) != 5 || strings.Count(statusMarkup, `aria-hidden="true"`) != 5 {
		t.Fatalf("protected status glyphs are not consistently silent: %s", statusMarkup)
	}
}

func TestTodo_UIPOLISH_010_Regression(t *testing.T) {
	css := Stylesheet()
	if !strings.Contains(css, ".nav-icon") || !strings.Contains(css, "height:20px;width:20px") {
		t.Fatal("existing navigation icon contract lost its canonical 20px target")
	}
	if got := iconPath("home"); got == fallbackIconPath || got == "" {
		t.Fatal("governed navigation icon no longer resolves")
	}
	if got := iconPath("unknown"); got != fallbackIconPath {
		t.Fatalf("unknown icon fallback changed to %q", got)
	}
}

func TestTodo_UIPOLISH_010_GovernedHeaderControls(t *testing.T) {
	for _, name := range []string{"search", "history-back", "history-forward"} {
		if got := iconPath(name); got == fallbackIconPath || got == "" {
			t.Errorf("header icon %q bypasses the governed vocabulary", name)
		}
	}
	history, err := ui.RenderToString(HistoryNavigation(HistoryNavigationProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(history, `class="history-navigation-glyph"`) != 2 || strings.Contains(history, ">←<") || strings.Contains(history, ">→<") {
		t.Fatalf("browser-history controls do not use governed decorative SVGs: %s", history)
	}
	if strings.Count(history, `aria-hidden="true"`) != 2 || strings.Count(history, `focusable="false"`) != 2 {
		t.Fatalf("browser-history glyphs are announced or focusable: %s", history)
	}
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	searchGlyph := strings.Index(doc, `class="global-search-glyph"`)
	if searchGlyph < 0 {
		t.Fatal("global search glyph missing from production render")
	}
	openSVG := strings.LastIndex(doc[:searchGlyph], "<svg")
	if openSVG < 0 || strings.Contains(doc[openSVG:searchGlyph], ">") {
		t.Fatal("global search glyph is not the governed inline SVG")
	}
	if !strings.Contains(Stylesheet(), `[dir=rtl] .history-navigation-glyph`) {
		t.Fatal("history-direction glyphs do not respect RTL")
	}
}

func TestTodo_UIPOLISH_010_GovernedMenuFilter(t *testing.T) {
	markup, err := ui.RenderToString(MenuFilter(MenuFilterProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="menu-filter-glyph"`) || strings.Contains(markup, "⌕") {
		t.Fatalf("menu filter does not use the governed search glyph: %s", markup)
	}
	if !strings.Contains(markup, `aria-hidden="true"`) || !strings.Contains(markup, `focusable="false"`) {
		t.Fatalf("decorative menu filter glyph is exposed to assistive technology: %s", markup)
	}
	if !strings.Contains(Stylesheet(), `.menu-filter-glyph{height:18px;width:18px;}`) {
		t.Fatal("menu filter glyph lost the shared optical size")
	}
}

func TestTodo_UIPOLISH_010_GovernedNavigationActions(t *testing.T) {
	for _, favorite := range []bool{false, true} {
		markup, err := ui.RenderToString(NavigationItem(NavigationItemProps{
			I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
			Label:     "People", Icon: "people", Href: "/workspace/app/people",
			FavoriteHref: "/workspace/app/people?favorite=true", Favorite: favorite,
		}))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(markup, `class="nav-favorite-glyph"`) || strings.Contains(markup, "☆") || strings.Contains(markup, "★") {
			t.Fatalf("favorite state %v bypasses the governed glyph: %s", favorite, markup)
		}
		if !strings.Contains(markup, `aria-hidden="true"`) || !strings.Contains(markup, `focusable="false"`) {
			t.Fatalf("favorite state %v exposes its decorative icon: %s", favorite, markup)
		}
		if favorite && !strings.Contains(markup, `class="nav-favorite is-favorite"`) {
			t.Fatalf("favorite state does not select the filled glyph style: %s", markup)
		}
	}
	group, err := ui.RenderToString(NavigationItem(NavigationItemProps{
		I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")},
		Label:     "My Work", Icon: "work", Children: []NavigationItemProps{{Label: "Open work", Href: "/workspace/app/work"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(group, `class="nav-chevron"`) || strings.Contains(group, "›") {
		t.Fatalf("navigation disclosure bypasses the governed glyph: %s", group)
	}
	css := Stylesheet()
	for _, want := range []string{`.nav-favorite-glyph{height:18px;width:18px;}`, `.nav-favorite.is-favorite .nav-favorite-glyph{fill:currentColor;}`, `[dir=rtl] .nav-group[open]>.nav-group-summary .nav-chevron{transform:scaleX(-1) rotate(90deg);}`} {
		if !strings.Contains(css, want) {
			t.Errorf("navigation glyph state missing %q", want)
		}
	}
}

func TestTodo_UIPOLISH_010_GovernedDirectionalControls(t *testing.T) {
	org, err := ui.RenderToString(organizationFlat([]OrganizationGroupProps{{Name: "Clinical", CountLabel: "12 employees"}}))
	if err != nil {
		t.Fatal(err)
	}
	work, err := ui.RenderToString(WorkRow(WorkRowProps{Title: "Review", Person: "Jane", Href: "/workspace/app/work"}))
	if err != nil {
		t.Fatal(err)
	}
	for name, markup := range map[string]string{"organization": org, "work": work} {
		if strings.Contains(markup, ">›<") || !strings.Contains(markup, `<svg`) || !strings.Contains(markup, `aria-hidden="true"`) {
			t.Errorf("%s direction control bypasses decorative governed SVG: %s", name, markup)
		}
	}
	css := Stylesheet()
	for _, fragment := range []string{
		`.organization-unit-chevron{height:16px;width:16px;}`,
		`[dir=rtl] .work-row-chevron`,
		`[dir=rtl] .organization-unit-disclosure[open]>.org-node.manager .organization-unit-glyph`,
	} {
		if !strings.Contains(css, fragment) {
			t.Errorf("direction control CSS missing %q", fragment)
		}
	}
	geometryFound := false
	for remaining := css; ; {
		start := strings.Index(remaining, `.work-row-chevron{`)
		if start < 0 {
			break
		}
		remaining = remaining[start:]
		end := strings.IndexByte(remaining, '}')
		if end < 0 {
			t.Fatal("work row chevron geometry rule is malformed")
		}
		rule := remaining[:end]
		if strings.Contains(rule, "height:16px") && strings.Contains(rule, "width:16px") && strings.Contains(rule, "flex:none") {
			geometryFound = true
		}
		remaining = remaining[end+1:]
	}
	if !geometryFound {
		t.Error("work row chevron has no bounded, nonshrinking geometry")
	}
	for _, column := range []string{"3", "2"} {
		pattern := `\.work-row>\.work-row-chevron\{[^}]*grid-column:` + column
		if !regexp.MustCompile(pattern).MatchString(css) {
			t.Errorf("work row chevron is unplaced at a responsive breakpoint: column %s", column)
		}
		statusPattern := `\.work-row:has\(\.status-dimensions\)>\.work-row-chevron\{[^}]*grid-column:` + column
		if !regexp.MustCompile(statusPattern).MatchString(css) {
			t.Errorf("status-projection work row chevron is unplaced at a responsive breakpoint: column %s", column)
		}
	}
	for _, pattern := range []string{
		`\.work-row:has\(\.status-dimensions\)\{[^}]*grid-template-columns:auto minmax\(0,1fr\) auto`,
		`\.work-row:has\(\.status-dimensions\)\{[^}]*grid-template-columns:minmax\(0,1fr\) auto`,
		`\.work-row:has\(\.status-dimensions\)>.row-main\{[^}]*grid-column:1 / -1`,
	} {
		if !regexp.MustCompile(pattern).MatchString(css) {
			t.Errorf("status-projection work row lost responsive grid override: %s", pattern)
		}
	}
}

func TestTodo_UIPOLISH_010_DisclosureGlyphs(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	rows := map[string]ui.Node{
		"context switcher":     ui.CreateElement(ContextSwitcher, web038Fixture()),
		"delegation selector":  ui.CreateElement(DelegationSelector, web055Fixture()),
		"sensitive details":    ui.CreateElement(SensitiveDetails, SensitiveDetailsProps{I18nProps: I18nProps{Locale: locale}, Title: "Private details", Description: "Hidden by default"}),
		"people workflow menu": ui.CreateElement(PeopleRow, PeopleRowProps{I18nProps: I18nProps{Locale: locale}, Name: "Jane Doe", QuickActions: []PeopleQuickActionProps{{Label: "Promotion", Href: "/workspace/app/journeys?mode=new"}, {Label: "Transfer", Href: "/workspace/app/journeys?mode=transfer"}}}), // two actions: one is offered directly, without a menu (UXLIVE-033)
	}
	classes := map[string]string{
		"context switcher":     "context-switcher-chevron",
		"delegation selector":  "delegation-selector-chevron",
		"sensitive details":    "sensitive-summary-chevron",
		"people workflow menu": "people-workflow-chevron",
	}
	for name, node := range rows {
		markup, err := ui.RenderToString(node)
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		if !strings.Contains(markup, `<svg`) || !strings.Contains(markup, `class="`+classes[name]+`"`) || !strings.Contains(markup, `aria-hidden="true"`) || strings.Contains(markup, "⌄") || strings.Contains(markup, ">›<") {
			t.Errorf("%s disclosure is not a silent governed icon: %s", name, markup)
		}
		if name == "people workflow menu" && !strings.Contains(markup, `aria-label="Promotion"`) {
			t.Errorf("people workflow link lost its accessible fallback label: %s", markup)
		}
		if name == "sensitive details" && (!strings.Contains(markup, `class="privacy-icon-glyph"`) || strings.Contains(markup, ">●<")) {
			t.Errorf("private-data marker bypasses the governed icon vocabulary: %s", markup)
		}
	}
	css := Stylesheet()
	for _, selector := range []string{
		`.context-switcher[open] .context-switcher-chevron`,
		`.delegation-selector[open] .delegation-selector-chevron`,
		`.sensitive-details[open]>.sensitive-summary .sensitive-summary-chevron`,
		`.people-workflow-menu[open]>summary .people-workflow-chevron`,
		`[dir=rtl] .sensitive-details[open]>.sensitive-summary .sensitive-summary-chevron`,
		`.privacy-icon-glyph{height:18px;width:18px;}`,
	} {
		if !strings.Contains(css, selector) {
			t.Errorf("disclosure state CSS missing %q", selector)
		}
	}
}

func TestTodo_UIPOLISH_010_WorkflowLauncherUsesGovernedIcon(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	markup, err := ui.RenderToString(ui.CreateElement(WorkflowCard, WorkflowCardProps{
		I18nProps: I18nProps{Locale: locale}, Name: "Promotion", Category: "Career", Description: "Propose a change.", Href: "/workspace/app/journeys?mode=new",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(markup, `class="workflow-icon-glyph"`) || !strings.Contains(markup, `aria-hidden="true"`) ||
		!strings.Contains(markup, locale.Text("workflow.start_named", map[string]string{"name": "Promotion"})) || strings.Contains(markup, "↗") {
		t.Fatalf("workflow launcher bypasses governed decorative icon: %s", markup)
	}
	if !strings.Contains(Stylesheet(), `.workflow-icon-glyph{height:20px;width:20px;}`) {
		t.Fatal("workflow icon loses its optical size")
	}
}

func TestTodo_UIPOLISH_010_SharedCheckGlyphsUseRegistry(t *testing.T) {
	if iconPath("check") == fallbackIconPath {
		t.Fatal("check has no governed icon definition")
	}
	activity, err := ui.RenderToString(ActivityList([]ActivityProps{{Title: "Completed", Status: "Recorded"}}, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	boundary, err := ui.RenderToString(SelfServiceBoundary(SelfServiceBoundaryProps{I18nProps: I18nProps{Locale: ResolveProductLocale("en-US")}}))
	if err != nil {
		t.Fatal(err)
	}
	for name, markup := range map[string]string{"activity": activity, "self-service": boundary} {
		if strings.Contains(markup, "✓") || !strings.Contains(markup, `aria-hidden="true"`) || !strings.Contains(markup, iconPath("check")) {
			t.Errorf("%s bypasses the governed decorative check icon: %s", name, markup)
		}
	}
}
