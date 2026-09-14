package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-194: authorized knowledge search. The
// registry owns every product surface, but no knowledge
// search exists: searching help knowledge with
// authorization filtering has no exposure point and the
// first surface invents search data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that searches
// nothing until the governed help service publishes,
// with knowledge truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_194(t *testing.T) {
	definition, ok := LookupPage(PageKnowledgeSearch)
	if !ok {
		t.Fatal("authorized knowledge search unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("authorized knowledge search incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageKnowledgeSearch {
		t.Fatal("authorized knowledge search route does not round-trip")
	}
	doc, err := Render(testView(PageKnowledgeSearch))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("authorized knowledge search exposes an unresolved message key")
	}
	for _, invented := range []string{"9 results for leave", "restricted article shown", "indexed yesterday", "search ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("authorized knowledge search invents search data: %q", invented)
		}
	}
}

// Golden: the registered knowledge search definition
// and its fallback copy.
func TestTodo_WEB_194_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageKnowledgeSearch)
	if !ok {
		t.Fatal("authorized knowledge search unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("knowledge_search.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("knowledge_search.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "e7a6936117cd77db202a25ea6f79748960394b0d619c7983ccd7924cfa713bb0"
	if got != want {
		t.Fatalf("authorized knowledge search digest = %s, want %s", got, want)
	}
}

// Browser: authorized knowledge search renders
// deterministically and round-trips its route.
func TestTodo_WEB_194_Browser(t *testing.T) {
	first, err := Render(testView(PageKnowledgeSearch))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageKnowledgeSearch))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("authorized knowledge search renders nondeterministically")
	}
	definition, _ := LookupPage(PageKnowledgeSearch)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageKnowledgeSearch {
		t.Fatal("authorized knowledge search route does not round-trip")
	}
}

// Conformance: authorized knowledge search keeps the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_194_Conformance(t *testing.T) {
	if !PageVisible(PageKnowledgeSearch, []string{"worker_self"}) {
		t.Fatal("authorized knowledge search hidden from the employee")
	}
	if PageVisible(PageKnowledgeSearch, nil) {
		t.Fatal("authorized knowledge search visible without roles")
	}
	definition, _ := LookupPage(PageKnowledgeSearch)
	if definition.PrimaryNav {
		t.Fatal("authorized knowledge search claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageKnowledgeSearch), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("authorized knowledge search leaks a key in %s", code)
		}
	}
}
