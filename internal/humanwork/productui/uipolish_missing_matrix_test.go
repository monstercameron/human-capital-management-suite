package productui

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RenderedDocument checks parse production server-rendered DOM. They cover
// document semantics without claiming browser interaction or painted output.
func TestTodo_UIPOLISH_002_RenderedDocument(t *testing.T) {
	view := testView(PageHome)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	grid := findElementsByClassContains(root, "home-grid")
	if len(grid) != 1 {
		t.Fatalf("home has %d home grids, want one", len(grid))
	}
	for _, class := range []string{"home-primary-rail", "home-supporting-rail"} {
		if len(findElementsByClassContains(grid[0], class)) != 1 {
			t.Errorf("home grid does not render exactly one %s rail", class)
		}
	}
	css := Stylesheet()
	if !strings.Contains(css, `:root[data-hcm-density="compact"] .home-grid`) || !strings.Contains(css, `:root[data-hcm-density="spacious"] .home-grid`) {
		t.Fatal("browser document's home layout has no compact and spacious density rules")
	}
}

func TestTodo_UIPOLISH_002_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(testView(PageHome), ResolveProductLocale(locale))
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("%s render: %v", locale, err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		main := firstElement(root, "main")
		h1 := firstElement(root, "h1")
		if main == nil || h1 == nil {
			t.Fatalf("%s home lost main landmark or page heading", locale)
		}
		if !strings.Contains(doc, `lang="`+view.Locale.Resolved+`"`) || !strings.Contains(doc, `dir="`+string(view.Locale.Direction)+`"`) {
			t.Errorf("%s page spacing composition lost locale reading direction", locale)
		}
	}
}

func TestTodo_UIPOLISH_002_Golden(t *testing.T) {
	css := Stylesheet()
	for _, exact := range []string{
		`@media (min-width:761px){:root[data-hcm-density="compact"] .home-grid,:root[data-hcm-density="compact"] .side-stack,:root[data-hcm-density="compact"] .workbench,:root[data-hcm-density="compact"] .insights-grid{gap:15px;}}`,
		`@media (min-width:761px){:root[data-hcm-density="spacious"] .home-grid,:root[data-hcm-density="spacious"] .side-stack,:root[data-hcm-density="spacious"] .workbench,:root[data-hcm-density="spacious"] .insights-grid{gap:25px;}}`,
	} {
		if !strings.Contains(css, exact) {
			t.Errorf("density golden rule changed or disappeared: %s", exact)
		}
	}
}

func TestTodo_UIPOLISH_006_RenderedDocument(t *testing.T) {
	for _, page := range []PageID{PageHome, PagePeople, PageSettings} {
		doc, err := Render(testView(page))
		if err != nil {
			t.Fatalf("%s: %v", page, err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		walkElements(root, func(n *xhtml.Node) {
			if n.Type != xhtml.ElementNode || (n.Data != "button" && n.Data != "input" && n.Data != "select" && n.Data != "textarea") {
				return
			}
			if attr(n, "disabled") == "" && attr(n, "aria-label") == "" && attr(n, "aria-labelledby") == "" && strings.TrimSpace(nodeText(n)) == "" && n.Data != "input" {
				t.Errorf("%s has an unlabeled interactive %s", page, n.Data)
			}
		})
	}
	css := Stylesheet()
	if !strings.Contains(css, `:focus-visible`) || !strings.Contains(css, `:disabled`) {
		t.Fatal("production controls omit visible focus or disabled state styling")
	}
}

func TestTodo_UIPOLISH_006_Performance(t *testing.T) {
	css := Stylesheet()
	if len(css) > 1_000_000 {
		t.Fatalf("production control stylesheet grew to %d bytes", len(css))
	}
	if strings.Contains(css, "transition:all") || strings.Contains(css, "transition: all") {
		t.Fatal("control styles use an unbounded transition")
	}
	if strings.Count(css, ":focus-visible") == 0 {
		t.Fatal("shared control stylesheet has no keyboard focus treatment")
	}
}

func TestTodo_UIPOLISH_006_Regression(t *testing.T) {
	css := Stylesheet()
	if !strings.Contains(css, ":is(button,input,select,textarea)") {
		t.Fatal("shared target selector no longer includes all native controls")
	}
	if !strings.Contains(css, "--hcm-control-height:44px") || !strings.Contains(css, "--hcm-control-height-compact:44px") {
		t.Fatal("base or compact control target fell below 44px")
	}
	if !strings.Contains(css, ".button.primary") || !strings.Contains(css, "color:var(--on-brand)") {
		t.Fatal("primary action lost its semantic foreground")
	}
}

func TestTodo_UIPOLISH_007_RenderedDocument(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(testView(PageHistory), ResolveProductLocale(locale))
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		if htmlEl := firstElement(root, "html"); htmlEl == nil || attr(htmlEl, "lang") != locale {
			t.Errorf("rendered browser document has wrong lang for %s", locale)
		}
		if strings.Contains(doc, "⟦") {
			t.Errorf("%s rendered unresolved product message key", locale)
		}
	}
}

func TestTodo_UIPOLISH_008_RenderedDocument(t *testing.T) {
	view := testView(PageHistory)
	view.Appearance = DefaultCustomerTheme()
	view.Appearance.Density = "compact"
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(findElementsByClassContains(root, "workflow-history")) != 1 {
		t.Fatal("production History document does not render exactly one history region")
	}
	if !strings.Contains(doc, `data-hcm-density="compact"`) {
		t.Fatal("production document did not apply persisted compact density")
	}
}

func TestTodo_UIPOLISH_008_Accessibility(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(testView(PageHistory), ResolveProductLocale(locale))
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		for _, table := range findElementsByClassContains(root, "data-table") {
			walkElements(table, func(n *xhtml.Node) {
				if n.Type == xhtml.ElementNode && n.Data == "th" && strings.TrimSpace(nodeText(n)) == "" {
					t.Errorf("%s table has an unnamed column heading", locale)
				}
			})
		}
		if !strings.Contains(doc, `dir="`+string(view.Locale.Direction)+`"`) {
			t.Errorf("%s table lost locale direction", locale)
		}
	}
}

func TestTodo_UIPOLISH_008_I18N(t *testing.T) {
	for _, locale := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageHistory), ResolveProductLocale(locale))
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("%s: %v", locale, err)
		}
		for _, key := range []string{"page.history.title", "history.search_aria", "history.all_people"} {
			message := view.Locale.Text(key)
			if message == "" || strings.Contains(message, "⟦") || !strings.Contains(doc, message) {
				t.Errorf("%s history omits translated %s = %q", locale, key, message)
			}
		}
	}
}

func TestTodo_UIPOLISH_008_Performance(t *testing.T) {
	css := uipolish008TableStylesheet()
	if len(css) > 100_000 {
		t.Fatalf("responsive table stylesheet is unexpectedly large: %d bytes", len(css))
	}
	if strings.Contains(css, "animation:") || strings.Contains(css, "transition:all") {
		t.Fatal("density rules animate layout or add unbounded transitions")
	}
	for _, density := range []string{"compact", "comfortable", "spacious"} {
		if _, ok := CustomerThemeAttributes(CustomerTheme{Density: density})["data-hcm-density"]; !ok {
			t.Errorf("%s has no root density attribute", density)
		}
	}
}

func TestTodo_UIPOLISH_008_Regression(t *testing.T) {
	css := uipolish008TableStylesheet()
	if strings.Contains(css, "display:none") || strings.Contains(css, "order:") {
		t.Fatal("responsive density changed visibility or reading order")
	}
	if !strings.Contains(Stylesheet(), ".data-table-scroll") {
		t.Fatal("production table no longer retains its horizontal scroll owner")
	}
	if !strings.Contains(css, "overflow-wrap:anywhere") {
		t.Fatal("narrow cells can again force content beyond the viewport")
	}
}

func TestTodo_UIPOLISH_012_RenderedDocument(t *testing.T) {
	for _, definition := range PageDefinitions() {
		doc, err := Render(testView(definition.ID))
		if err != nil {
			t.Fatalf("%s: %v", definition.ID, err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatalf("%s: %v", definition.ID, err)
		}
		if firstElement(root, "main") == nil || firstElement(root, "h1") == nil {
			t.Errorf("%s browser document lacks main or page heading", definition.ID)
		}
		if !strings.Contains(doc, `data-hcm-catalog="product-ui.v1"`) {
			t.Errorf("%s bypasses production UI catalog", definition.ID)
		}
	}
}

func TestTodo_UIPOLISH_012_Golden(t *testing.T) {
	view := testView(PageHome)
	doc, err := Render(view)
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(doc)))
	const want = "ee2e99d3d0b8918b680af335ebde26c05fed4d48aac420cec00f446b1d0a32e2"
	if digest != want {
		t.Fatalf("production Home document golden = %s, want %s", digest, want)
	}
}
