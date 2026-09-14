package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-196: confidential case intake. The
// registry owns every product surface, but no
// confidential intake exists: raising a confidential
// case has no exposure point and the first surface
// invents intake data by convention. The compiler needs
// the registered surface — canonical identity, route,
// and an honest fallback that intakes nothing until the
// governed help service publishes, with case truth
// staying server authority — so the surface resolves
// today without a second source of business authority.
func TestTodo_WEB_196(t *testing.T) {
	definition, ok := LookupPage(PageConfidentialCase)
	if !ok {
		t.Fatal("confidential case intake unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("confidential case intake incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageConfidentialCase {
		t.Fatal("confidential case intake route does not round-trip")
	}
	doc, err := Render(testView(PageConfidentialCase))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("confidential case intake exposes an unresolved message key")
	}
	for _, invented := range []string{"sealed case 77", "sensitive matter filed", "visible to Tarek", "confidential ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("confidential case intake invents intake data: %q", invented)
		}
	}
}

// Golden: the registered confidential intake definition
// and its fallback copy.
func TestTodo_WEB_196_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageConfidentialCase)
	if !ok {
		t.Fatal("confidential case intake unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("confidential_case.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("confidential_case.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "bb72d65fabd09caf9558b7121ad1f6e43a3bd60a8727c03361d8ec5b1f0d1713"
	if got != want {
		t.Fatalf("confidential case intake digest = %s, want %s", got, want)
	}
}

// Browser: confidential case intake renders
// deterministically and round-trips its route.
func TestTodo_WEB_196_Browser(t *testing.T) {
	first, err := Render(testView(PageConfidentialCase))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageConfidentialCase))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("confidential case intake renders nondeterministically")
	}
	definition, _ := LookupPage(PageConfidentialCase)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageConfidentialCase {
		t.Fatal("confidential case intake route does not round-trip")
	}
}

// Conformance: confidential case intake keeps the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_196_Conformance(t *testing.T) {
	if !PageVisible(PageConfidentialCase, []string{"worker_self"}) {
		t.Fatal("confidential case intake hidden from the employee")
	}
	if PageVisible(PageConfidentialCase, nil) {
		t.Fatal("confidential case intake visible without roles")
	}
	definition, _ := LookupPage(PageConfidentialCase)
	if definition.PrimaryNav {
		t.Fatal("confidential case intake claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageConfidentialCase), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("confidential case intake leaks a key in %s", code)
		}
	}
}
