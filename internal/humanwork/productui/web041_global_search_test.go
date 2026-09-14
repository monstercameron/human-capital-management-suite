package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"
)

// RED for WEB-041: authorization-safe global search. The shell must offer
// one search surface whose results never escape the authorized navigation
// projection, whose destinations stay inside the shell, and whose derivation
// is deterministic for one view.

func TestTodo_WEB_041(t *testing.T) {
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	search := findSearchForm(root)
	if search == nil {
		t.Fatal("shell rendered no global search surface")
	}

	// The full item set answers people, pages, workflows, and settings
	// queries from versioned view inputs.
	items := globalSearchItems(testView(PageHome))
	for query, id := range map[string]string{
		"Avery":             "person:worker-avery",
		"setings":           "page:settings",
		"dark mode":         "component:brand-appearance",
		"internal transfer": "workflow:transfer",
	} {
		results := SearchGlobalItems(items, query, globalSearchLimit)
		if !hasSearchResult(results, id) {
			t.Fatalf("SearchGlobalItems(%q) missing %q: %#v", query, id, searchResultIDs(results))
		}
	}

	// Every advertised destination stays inside the shell on a registered
	// route; search never links out or invents a destination.
	for _, item := range items {
		parsed, err := parseSearchHref(item.Href)
		if err != nil || parsed == "" {
			t.Fatalf("search item %q has an out-of-shell destination %q", item.ID, item.Href)
		}
	}

	// A projection without People hides person results but keeps the honest
	// surface and the shell support pages.
	view := testView(PageHome)
	view.Navigation = view.Navigation[:2]
	denied := globalSearchItems(view)
	for _, result := range SearchGlobalItems(denied, "Avery", globalSearchLimit) {
		if result.Kind == "person" || strings.HasPrefix(result.ID, "person:") || strings.HasPrefix(result.ID, "action:promotion:") {
			t.Fatalf("restricted projection leaked person result %q", result.ID)
		}
	}
	if results := SearchGlobalItems(denied, "settings", globalSearchLimit); !hasSearchResult(results, "page:settings") {
		t.Fatal("restricted projection lost the shell support pages")
	}

	// Successive derivations from the same view agree; search keeps no
	// mutable projection a caller could poison between renders.
	first := globalSearchItems(testView(PageHome))
	second := globalSearchItems(testView(PageHome))
	if len(first) != len(second) || strings.Join(searchResultIDs(first), ",") != strings.Join(searchResultIDs(second), ",") {
		t.Fatal("search derivation is not deterministic for one view")
	}
}

func parseSearchHref(href string) (string, error) {
	if !strings.HasPrefix(href, "/workspace/app/") {
		return "", fmt.Errorf("out of shell")
	}
	return href, nil
}

func findSearchForm(root *xhtml.Node) *xhtml.Node {
	var found *xhtml.Node
	walkElements(root, func(node *xhtml.Node) {
		if found == nil && node.Data == "form" && attr(node, "role") == "search" {
			found = node
		}
	})
	return found
}

// web041GoldenDigest is pinned from the GREEN implementation run.
const web041GoldenDigest = "c8c983c19d30b613989bdc539a6099b9a42e88d1f8f9c2607e5ff9373c5a64a2"

func TestTodo_WEB_041_Golden(t *testing.T) {
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	search := findSearchForm(root)
	if search == nil {
		t.Fatal("shell rendered no global search surface")
	}
	var rendered strings.Builder
	if err := xhtml.Render(&rendered, search); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(rendered.String()))
	if got := hex.EncodeToString(digest[:]); got != web041GoldenDigest {
		t.Fatalf("search golden digest mismatch: got %s want %s", got, web041GoldenDigest)
	}
}

func TestTodo_WEB_041_Browser(t *testing.T) {
	view := testView(PageHome)
	props := globalSearchProps(view)
	props.InitialQuery = "Avery"
	markup, err := ui.RenderToString(ui.CreateElement(GlobalSearch, props))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`role="combobox"`, `aria-autocomplete="list"`, `aria-expanded="true"`,
		`role="listbox"`, `role="option"`, `aria-selected="true"`,
		`aria-label="Search Human Capital Management Suite"`, `href="/workspace/app/person?person=worker-avery"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("global search markup missing %q", want)
		}
	}
	css := Stylesheet()
	for _, want := range []string{
		`.global-search{grid-column:1 / -1;`,
		`.global-search-panel{animation-duration:var(--hcm-motion-fast,.14s);`,
		`.global-search-result.active{box-shadow:inset 3px 0 0 0 var(--accent);}`,
		`@media (forced-colors:active){.global-search-input,.global-search-panel,.global-search-result{border:1px solid CanvasText;}`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("search stylesheet missing %q", want)
		}
	}
}

func TestTodo_WEB_041_Conformance(t *testing.T) {
	// Narrow props: the search contract must never carry the page-wide View.
	if typeContains(reflect.TypeOf(GlobalSearchProps{}), reflect.TypeOf(View{}), map[reflect.Type]bool{}) {
		t.Fatal("GlobalSearchProps embeds the page-wide View instead of narrow props")
	}
	// Every locale renders named search copy; unresolved keys stay visible
	// as ⟦key⟧ and must never ship in the shell.
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		view := ApplyLocale(testView(PageHome), ResolveProductLocale(locale))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		root, err := xhtml.Parse(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		search := findSearchForm(root)
		if search == nil {
			t.Fatalf("locale %s rendered no global search surface", locale)
		}
		var rendered strings.Builder
		if err := xhtml.Render(&rendered, search); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(rendered.String(), "⟦") {
			t.Fatalf("locale %s rendered an unresolved search message key", locale)
		}
	}
}
