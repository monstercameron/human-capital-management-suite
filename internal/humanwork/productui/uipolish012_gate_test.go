package productui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/wcag"
	xhtml "golang.org/x/net/html"
)

// These dimensions prove server-rendered composition and document contracts.
// They do not claim browser, visual-baseline, or assistive-technology
// execution; those require a live evidence runner and remain release work.
type uiQualificationPersona struct {
	name  string
	roles []string
}

var uiQualificationPersonas = []uiQualificationPersona{
	{name: "employee", roles: []string{"worker_self"}},
	{name: "manager", roles: []string{"manager"}},
	{name: "hr-partner", roles: []string{"hr_partner"}},
	{name: "administrator", roles: []string{RoleHCMAdmin}},
}

func TestTodo_UIPOLISH_012(t *testing.T) {
	definitions := PageDefinitions()
	if len(definitions) < 6 {
		t.Fatalf("production page registry contains %d pages; want at least 6", len(definitions))
	}
	seen := map[PageID]bool{}
	for _, definition := range definitions {
		if definition.ID == "" || definition.Route == "" || definition.render == nil {
			t.Fatalf("incomplete production page definition: %+v", definition)
		}
		if seen[definition.ID] {
			t.Fatalf("duplicate production page identity %q", definition.ID)
		}
		seen[definition.ID] = true
		roundTrip, ok := LookupRoute(definition.Route)
		if !ok || roundTrip.ID != definition.ID {
			t.Fatalf("route %q does not round-trip to %q", definition.Route, definition.ID)
		}
	}
	for _, token := range []string{"--hcm-font-sans", "--hcm-font-size-body", "--hcm-radius-surface", "--hcm-shadow-resting"} {
		if !strings.Contains(Stylesheet(), token) {
			t.Fatalf("production stylesheet missing semantic token %s", token)
		}
	}
}

// Every registry page and admitted persona gets a real production SSR render.
func TestTodo_UIPOLISH_012_SSRMatrix(t *testing.T) {
	for _, persona := range uiQualificationPersonas {
		persona := persona
		for _, definition := range PageDefinitions() {
			definition := definition
			t.Run(fmt.Sprintf("%s/%s", persona.name, definition.ID), func(t *testing.T) {
				admitted := PageVisible(definition.ID, persona.roles)
				view := ApplyLocale(ApplyRoleVisibility(testView(definition.ID), persona.roles), ResolveProductLocale("en-US"))
				doc, err := Render(view)
				if err != nil {
					t.Fatalf("SSR render: %v", err)
				}
				assertUIPolishDocument(t, doc, view.Locale)
				if !admitted && ui012NavigationContains(view.Navigation, definition.ID) {
					t.Fatalf("unauthorized page %q is discoverable for %s", definition.ID, persona.name)
				}
			})
		}
	}
}

func TestTodo_UIPOLISH_012_Accessibility(t *testing.T) {
	for _, definition := range PageDefinitions() {
		definition := definition
		t.Run(string(definition.ID), func(t *testing.T) {
			view := ApplyLocale(testView(definition.ID), ResolveProductLocale("en-US"))
			doc, err := Render(view)
			if err != nil {
				t.Fatal(err)
			}
			for _, result := range wcag.SurfaceScore(doc, view.Locale.Resolved, string(view.Locale.Direction)) {
				if !result.Pass {
					t.Errorf("%s: %s", result.Name, result.Detail)
				}
			}
		})
	}
}

func TestTodo_UIPOLISH_012_I18N(t *testing.T) {
	for _, code := range SupportedProductLocales() {
		for _, definition := range PageDefinitions() {
			code, definition := code, definition
			t.Run(fmt.Sprintf("%s/%s", code, definition.ID), func(t *testing.T) {
				view := ApplyLocale(testView(definition.ID), ResolveProductLocale(code))
				doc, err := Render(view)
				if err != nil {
					t.Fatal(err)
				}
				if result := wcag.CheckLocale(doc, view.Locale.Resolved, string(view.Locale.Direction)); !result.Pass {
					t.Fatal(result.Detail)
				}
				if strings.Contains(doc, "TODO") || strings.Contains(doc, "⟦") {
					t.Fatal("technical placeholder content reached rendered page")
				}
			})
		}
	}
}

// Performance checks are limited to deterministic static contracts. Render
// latency and layout shift cannot be inferred from Go execution time.
func TestTodo_UIPOLISH_012_Performance(t *testing.T) {
	css := Stylesheet()
	if strings.TrimSpace(css) == "" {
		t.Fatal("production stylesheet is empty")
	}
	if strings.Contains(strings.ToLower(css), "transition: all") {
		t.Fatal("unbounded transition: all violates interaction budget contract")
	}
	for _, definition := range PageDefinitions() {
		view := ApplyLocale(testView(definition.ID), ResolveProductLocale("en-US"))
		doc, err := Render(view)
		if err != nil {
			t.Fatalf("%s: %v", definition.ID, err)
		}
		if !strings.Contains(doc, `data-hcm-catalog="product-ui.v1"`) {
			t.Fatalf("%s: missing production catalog marker", definition.ID)
		}
	}
}

func TestTodo_UIPOLISH_012_Regression(t *testing.T) {
	for _, definition := range PageDefinitions() {
		view := testView(definition.ID)
		view.LoadError = "private-backend-sentinel"
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		assertUIPolishDocument(t, doc, view.Locale)
		if strings.Contains(doc, view.LoadError) {
			t.Fatalf("%s leaked raw backend failure", definition.ID)
		}
	}
}

func ui012NavigationContains(items []NavItem, page PageID) bool {
	for _, item := range items {
		if item.Page == page || ui012NavigationContains(item.Children, page) {
			return true
		}
	}
	return false
}

func assertUIPolishDocument(t *testing.T, doc string, locale LocaleContext) {
	t.Helper()
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	h1, main := 0, 0
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.ElementNode {
			if node.Data == "h1" {
				h1++
			}
			if node.Data == "main" {
				main++
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	if h1 != 1 || main != 1 {
		t.Fatalf("semantic hierarchy h1=%d main=%d", h1, main)
	}
	if !strings.Contains(doc, `lang="`+locale.Resolved+`"`) || !strings.Contains(doc, `dir="`+string(locale.Direction)+`"`) {
		t.Fatal("locale direction metadata missing")
	}
}
