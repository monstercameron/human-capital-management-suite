package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-166: benefit-plan comparison. The registry
// owns every product surface, but no plan comparison
// exists: comparing benefit plans has no exposure point
// and the first surface invents plan data by convention.
// The compiler needs the registered surface — canonical
// identity, route, and an honest fallback that compares
// nothing until the governed benefits service publishes,
// with plan truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_166(t *testing.T) {
	definition, ok := LookupPage(PageBenefitsCompare)
	if !ok {
		t.Fatal("benefit-plan comparison unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("benefit-plan comparison incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageBenefitsCompare {
		t.Fatal("benefits comparison route does not round-trip")
	}
	doc, err := Render(testView(PageBenefitsCompare))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("benefits comparison exposes an unresolved message key")
	}
	for _, invented := range []string{"plan A:", "versus:", "$0/mo", "better ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("benefits comparison invents plan data: %q", invented)
		}
	}
}

// Golden: the registered benefits comparison definition
// and its fallback copy.
func TestTodo_WEB_166_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageBenefitsCompare)
	if !ok {
		t.Fatal("benefit-plan comparison unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("benefits_compare.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("benefits_compare.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "3187aaca8c03c9bce8cdfdebb16edbcc6e04c30326b770359c7619ca48895e25"
	if got != want {
		t.Fatalf("benefits comparison digest = %s, want %s", got, want)
	}
}

// Browser: the benefits comparison renders
// deterministically and round-trips its route.
func TestTodo_WEB_166_Browser(t *testing.T) {
	first, err := Render(testView(PageBenefitsCompare))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageBenefitsCompare))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("benefits comparison renders nondeterministically")
	}
	definition, _ := LookupPage(PageBenefitsCompare)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageBenefitsCompare {
		t.Fatal("benefits comparison route does not round-trip")
	}
}

// Conformance: the benefits comparison keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_166_Conformance(t *testing.T) {
	if !PageVisible(PageBenefitsCompare, []string{"worker_self"}) {
		t.Fatal("benefits comparison hidden from the employee")
	}
	if PageVisible(PageBenefitsCompare, nil) {
		t.Fatal("benefits comparison visible without roles")
	}
	definition, _ := LookupPage(PageBenefitsCompare)
	if definition.PrimaryNav {
		t.Fatal("benefits comparison claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageBenefitsCompare), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("benefits comparison leaks a key in %s", code)
		}
	}
}
