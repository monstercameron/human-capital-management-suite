package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-159: pay-discrepancy intake. The registry
// owns every product surface, but no discrepancy intake
// exists: reporting a pay problem has no exposure point
// and the first surface invents case state by convention.
// The compiler needs the registered surface — canonical
// identity, route, and an honest fallback that files
// nothing until the governed pay service publishes, with
// case truth staying server authority — so the surface
// resolves today without a second source of business
// authority.
func TestTodo_WEB_159(t *testing.T) {
	definition, ok := LookupPage(PagePayDiscrepancy)
	if !ok {
		t.Fatal("pay-discrepancy intake unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("pay-discrepancy intake incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PagePayDiscrepancy {
		t.Fatal("pay-discrepancy route does not round-trip")
	}
	doc, err := Render(testView(PagePayDiscrepancy))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("pay-discrepancy intake exposes an unresolved message key")
	}
	for _, invented := range []string{"case:", "open cases", "filed:", "received ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("pay-discrepancy intake invents case data: %q", invented)
		}
	}
}

// Golden: the registered discrepancy intake definition
// and its fallback copy.
func TestTodo_WEB_159_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PagePayDiscrepancy)
	if !ok {
		t.Fatal("pay-discrepancy intake unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("pay_discrepancy.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("pay_discrepancy.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "598d2326763cf8533510c53b6f85c666f6dfe895f0216f7fb67eb2f738895ba3"
	if got != want {
		t.Fatalf("pay-discrepancy digest = %s, want %s", got, want)
	}
}

// Browser: discrepancy intake renders deterministically
// and round-trips its route.
func TestTodo_WEB_159_Browser(t *testing.T) {
	first, err := Render(testView(PagePayDiscrepancy))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PagePayDiscrepancy))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("pay-discrepancy intake renders nondeterministically")
	}
	definition, _ := LookupPage(PagePayDiscrepancy)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PagePayDiscrepancy {
		t.Fatal("pay-discrepancy route does not round-trip")
	}
}

// Conformance: discrepancy intake keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_159_Conformance(t *testing.T) {
	if !PageVisible(PagePayDiscrepancy, []string{"worker_self"}) {
		t.Fatal("pay-discrepancy intake hidden from the employee")
	}
	if PageVisible(PagePayDiscrepancy, nil) {
		t.Fatal("pay-discrepancy intake visible without roles")
	}
	definition, _ := LookupPage(PagePayDiscrepancy)
	if definition.PrimaryNav {
		t.Fatal("pay-discrepancy intake claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PagePayDiscrepancy), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("pay-discrepancy intake leaks a key in %s", code)
		}
	}
}
