package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// RED for WEB-043: account locale and accessibility controls. The settings
// surface must offer a locale panel that preserves navigation state across
// switches and an accessibility panel of native labelled controls, both
// narrow-propped, localized, and deterministic.

func TestTodo_WEB_043(t *testing.T) {
	doc, err := Render(testView(PageSettings))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	locale := findByClass(root, "locale-preferences")
	if locale == nil {
		t.Fatal("settings rendered no locale preferences panel")
	}
	if findByID(locale, "locale-preferences-title") == nil {
		t.Fatal("locale panel has no labelled heading")
	}
	access := findByClass(root, "accessibility-preferences")
	if access == nil {
		t.Fatal("settings rendered no accessibility preferences panel")
	}
	if findByID(access, "accessibility-title") == nil {
		t.Fatal("accessibility panel has no labelled heading")
	}

	// Every supported locale is offered with its own language and direction;
	// the Arabic option carries dir="rtl" while the panel stays usable.
	var choices []string
	walkElements(locale, func(node *xhtml.Node) {
		if node.Data == "a" && strings.Contains(attr(node, "class"), "locale-choice") {
			choices = append(choices, attr(node, "href"))
		}
	})
	if len(choices) != len(SupportedProductLocales()) {
		t.Fatalf("locale panel offers %d locales, want %d", len(choices), len(SupportedProductLocales()))
	}
	ar := ApplyLocale(testView(PageSettings), ResolveProductLocale("ar"))
	arDoc, err := Render(ar)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`dir="rtl"`, `lang="ar"`} {
		if !strings.Contains(arDoc, want) {
			t.Fatalf("arabic settings missing %q", want)
		}
	}

	// Accessibility choices are native radios with exactly one checked per
	// group, each labelled by its own label element.
	groups := 0
	walkElements(access, func(node *xhtml.Node) {
		if node.Data == "fieldset" {
			groups++
			checked := 0
			total := 0
			walkElements(node, func(input *xhtml.Node) {
				if input.Data == "input" && attr(input, "type") == "radio" {
					total++
					if hasAttr(input, "checked") {
						checked++
					}
					if strings.TrimSpace(attr(input, "id")) == "" || findLabelFor(access, attr(input, "id")) == nil {
						t.Errorf("radio %q has no labelling label", attr(input, "name"))
					}
				}
			})
			if total == 0 || checked != 1 {
				t.Errorf("fieldset has %d radios with %d checked, want exactly one checked", total, checked)
			}
		}
	})
	if groups < 4 {
		t.Fatalf("accessibility panel has %d choice groups, want at least text/contrast/motion/links", groups)
	}
}

func findByClass(root *xhtml.Node, class string) *xhtml.Node {
	var found *xhtml.Node
	walkElements(root, func(node *xhtml.Node) {
		if found != nil {
			return
		}
		for _, field := range strings.Fields(attr(node, "class")) {
			if field == class {
				found = node
				return
			}
		}
	})
	return found
}

func findByID(root *xhtml.Node, id string) *xhtml.Node {
	var found *xhtml.Node
	walkElements(root, func(node *xhtml.Node) {
		if found == nil && attr(node, "id") == id {
			found = node
		}
	})
	return found
}

func findLabelFor(root *xhtml.Node, id string) *xhtml.Node {
	var found *xhtml.Node
	walkElements(root, func(node *xhtml.Node) {
		if found == nil && node.Data == "label" && attr(node, "for") == id {
			found = node
		}
	})
	return found
}

// web043GoldenDigest is pinned from the GREEN implementation run.
const web043GoldenDigest = "51fab1e9353c09aa90f152064cac734c6f76a22d89a552b52c090811382361cb"

func TestTodo_WEB_043_Golden(t *testing.T) {
	doc, err := Render(testView(PageSettings))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	var rendered strings.Builder
	for _, class := range []string{"locale-preferences", "accessibility-preferences"} {
		section := findByClass(root, class)
		if section == nil {
			t.Fatalf("settings rendered no %s section", class)
		}
		if err := xhtml.Render(&rendered, section); err != nil {
			t.Fatal(err)
		}
	}
	digest := sha256.Sum256([]byte(rendered.String()))
	if got := hex.EncodeToString(digest[:]); got != web043GoldenDigest {
		t.Fatalf("settings controls golden digest mismatch: got %s want %s", got, web043GoldenDigest)
	}
}

func TestTodo_WEB_043_Browser(t *testing.T) {
	doc, err := Render(testView(PageSettings))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`aria-labelledby="locale-preferences-title"`, `aria-labelledby="accessibility-title"`,
		`role="status"`, `aria-live="polite"`, `type="radio"`, `<fieldset`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("settings controls markup missing %q", want)
		}
	}
	css := Stylesheet()
	for _, want := range []string{
		`.locale-preferences{overflow:hidden;}`,
		`.accessibility-preferences{overflow:hidden;}`,
		`.locale-choice-list{`,
		`.accessibility-options{`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("controls stylesheet missing %q", want)
		}
	}
}

func TestTodo_WEB_043_Conformance(t *testing.T) {
	// Narrow props: neither panel may carry the page-wide View.
	for _, typ := range []reflect.Type{reflect.TypeOf(LocalePreferencesProps{}), reflect.TypeOf(AccessibilityPreferencesProps{})} {
		if typeContains(typ, reflect.TypeOf(View{}), map[reflect.Type]bool{}) {
			t.Fatalf("%s embeds the page-wide View instead of narrow props", typ.Name())
		}
	}
	// Every locale renders both panels with no unresolved keys.
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(testView(PageSettings), ResolveProductLocale(locale))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		for _, class := range []string{"locale-preferences", "accessibility-preferences"} {
			section := findByClass(root, class)
			if section == nil {
				t.Fatalf("locale %s rendered no %s section", locale, class)
			}
			var rendered strings.Builder
			if err := xhtml.Render(&rendered, section); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(rendered.String(), "⟦") {
				t.Fatalf("locale %s rendered an unresolved %s message key", locale, class)
			}
		}
	}
}
