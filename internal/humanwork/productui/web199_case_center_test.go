package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-199: the specialist Case Center. The
// registry owns every product surface, but no Case
// Center exists: working cases as a specialist has no
// exposure point and the first surface invents case
// data by convention. The compiler needs the registered
// surface — canonical identity, route, and an honest
// fallback that centers nothing until the governed help
// service publishes, with case truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_199(t *testing.T) {
	definition, ok := LookupPage(PageCaseCenter)
	if !ok {
		t.Fatal("specialist Case Center unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("specialist Case Center incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageCaseCenter {
		t.Fatal("specialist Case Center route does not round-trip")
	}
	doc, err := Render(testView(PageCaseCenter))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("specialist Case Center exposes an unresolved message key")
	}
	for _, invented := range []string{"queue has 14 cases", "SLA breached twice", "owned by Karim", "center ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("specialist Case Center invents case data: %q", invented)
		}
	}
}

// Golden: the registered Case Center definition and
// its fallback copy.
func TestTodo_WEB_199_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageCaseCenter)
	if !ok {
		t.Fatal("specialist Case Center unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("case_center.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("case_center.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "ed2e6065dc1371d33efd9e390e6dc137c387499bf3348a12aad33f348c69da80"
	if got != want {
		t.Fatalf("specialist Case Center digest = %s, want %s", got, want)
	}
}

// Browser: specialist Case Center renders
// deterministically and round-trips its route.
func TestTodo_WEB_199_Browser(t *testing.T) {
	first, err := Render(testView(PageCaseCenter))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageCaseCenter))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("specialist Case Center renders nondeterministically")
	}
	definition, _ := LookupPage(PageCaseCenter)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageCaseCenter {
		t.Fatal("specialist Case Center route does not round-trip")
	}
}

// Conformance: specialist Case Center keeps the
// registry contract — visible to the HR specialist,
// hidden from the role-less baseline, ordered, and
// honest in every locale.
func TestTodo_WEB_199_Conformance(t *testing.T) {
	if !PageVisible(PageCaseCenter, []string{"hr_partner"}) {
		t.Fatal("specialist Case Center hidden from the HR specialist")
	}
	if PageVisible(PageCaseCenter, nil) {
		t.Fatal("specialist Case Center visible without roles")
	}
	definition, _ := LookupPage(PageCaseCenter)
	if definition.PrimaryNav {
		t.Fatal("specialist Case Center claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageCaseCenter), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("specialist Case Center leaks a key in %s", code)
		}
	}
}
