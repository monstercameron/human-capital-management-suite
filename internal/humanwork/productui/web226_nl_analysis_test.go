package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-226: safe natural-language analysis. The
// registry owns every product surface, but no
// natural-language analysis exists: asking governed
// questions in plain language has no exposure point and
// the first surface invents answer data by convention.
// The compiler needs the registered surface — canonical
// identity, route, and an honest fallback that answers
// nothing until the governed reporting service
// publishes, with answer truth staying server authority
// — so the surface resolves today without a second
// source of business authority.
func TestTodo_WEB_226(t *testing.T) {
	definition, ok := LookupPage(PageNLAnalysis)
	if !ok {
		t.Fatal("safe natural-language analysis unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("safe natural-language analysis incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageNLAnalysis {
		t.Fatal("safe natural-language analysis route does not round-trip")
	}
	doc, err := Render(testView(PageNLAnalysis))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("safe natural-language analysis exposes an unresolved message key")
	}
	for _, invented := range []string{"attrition is 8 pct", "asked by Dara", "answer pinned", "answer ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("safe natural-language analysis invents answer data: %q", invented)
		}
	}
}

// Golden: the registered natural-language definition
// and its fallback copy.
func TestTodo_WEB_226_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageNLAnalysis)
	if !ok {
		t.Fatal("safe natural-language analysis unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("nl_analysis.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("nl_analysis.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "402dcb34e6c079c37020ba34afe7b06cb579d365d20cf9dd0b562e7309a1fadb"
	if got != want {
		t.Fatalf("safe natural-language analysis digest = %s, want %s", got, want)
	}
}

// Browser: safe natural-language analysis renders
// deterministically and round-trips its route.
func TestTodo_WEB_226_Browser(t *testing.T) {
	first, err := Render(testView(PageNLAnalysis))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageNLAnalysis))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("safe natural-language analysis renders nondeterministically")
	}
	definition, _ := LookupPage(PageNLAnalysis)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageNLAnalysis {
		t.Fatal("safe natural-language analysis route does not round-trip")
	}
}

// Conformance: safe natural-language analysis keeps the
// registry contract — visible to the manager, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_226_Conformance(t *testing.T) {
	if !PageVisible(PageNLAnalysis, []string{"manager"}) {
		t.Fatal("safe natural-language analysis hidden from the manager")
	}
	if PageVisible(PageNLAnalysis, nil) {
		t.Fatal("safe natural-language analysis visible without roles")
	}
	definition, _ := LookupPage(PageNLAnalysis)
	if definition.PrimaryNav {
		t.Fatal("safe natural-language analysis claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageNLAnalysis), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("safe natural-language analysis leaks a key in %s", code)
		}
	}
}
