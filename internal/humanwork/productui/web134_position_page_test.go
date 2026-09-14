package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-134: the position-request page. The
// registry owns every product surface, but no position
// page exists: the position lifecycle has no exposure
// point and the first surface invents position data by
// convention. The compiler needs the registered page —
// canonical identity, route, and an honest fallback that
// exposes no positions until the governed position
// service publishes — so the page resolves today without
// a second source of business authority.
func TestTodo_WEB_134(t *testing.T) {
	definition, ok := LookupPage(PagePosition)
	if !ok {
		t.Fatal("position page unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("position page incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PagePosition {
		t.Fatal("position route does not round-trip")
	}
	doc, err := Render(testView(PagePosition))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("position page exposes an unresolved message key")
	}
	for _, invented := range []string{"POS-", "position #", "openings", "vacancies: 3"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("position page invents position data: %q", invented)
		}
	}
}

// Golden: the registered position definition and its
// fallback copy.
func TestTodo_WEB_134_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PagePosition)
	if !ok {
		t.Fatal("position page unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("position.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("position.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "d9b4df584f38661866c9ef6e099c96d7850fcbd45373124f5409fe1a6ba00fbc"
	if got != want {
		t.Fatalf("position digest = %s, want %s", got, want)
	}
}

// Browser: the position page renders deterministically
// and round-trips its route.
func TestTodo_WEB_134_Browser(t *testing.T) {
	first, err := Render(testView(PagePosition))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PagePosition))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("position page renders nondeterministically")
	}
	definition, _ := LookupPage(PagePosition)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PagePosition {
		t.Fatal("position route does not round-trip")
	}
}

// Conformance: the position page keeps the registry
// contract — visible to hiring roles, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_134_Conformance(t *testing.T) {
	if !PageVisible(PagePosition, []string{"hiring_manager"}) {
		t.Fatal("position page hidden from hiring managers")
	}
	if PageVisible(PagePosition, nil) {
		t.Fatal("position page visible without roles")
	}
	definition, _ := LookupPage(PagePosition)
	if definition.PrimaryNav {
		t.Fatal("position page claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PagePosition), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("position page leaks a key in %s", code)
		}
	}
}
