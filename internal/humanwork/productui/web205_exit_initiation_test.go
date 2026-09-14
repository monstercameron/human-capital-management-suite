package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-205: exit initiation. The registry owns
// every product surface, but no exit initiation exists:
// starting a worker exit has no exposure point and the
// first surface invents exit data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that initiates
// nothing until the governed lifecycle service
// publishes, with exit truth staying server authority —
// so the surface resolves today without a second source
// of business authority.
func TestTodo_WEB_205(t *testing.T) {
	definition, ok := LookupPage(PageExitInitiation)
	if !ok {
		t.Fatal("exit initiation unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("exit initiation incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageExitInitiation {
		t.Fatal("exit initiation route does not round-trip")
	}
	doc, err := Render(testView(PageExitInitiation))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("exit initiation exposes an unresolved message key")
	}
	for _, invented := range []string{"last day June 30", "exit interview set", "clearance 3 of 9", "exit ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("exit initiation invents exit data: %q", invented)
		}
	}
}

// Golden: the registered exit initiation definition
// and its fallback copy.
func TestTodo_WEB_205_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageExitInitiation)
	if !ok {
		t.Fatal("exit initiation unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("exit_initiation.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("exit_initiation.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "ff8d04f1427b7250a35557f8e1a79a352f374955dbe9c31f5b0b93ffbe1969aa"
	if got != want {
		t.Fatalf("exit initiation digest = %s, want %s", got, want)
	}
}

// Browser: exit initiation renders deterministically
// and round-trips its route.
func TestTodo_WEB_205_Browser(t *testing.T) {
	first, err := Render(testView(PageExitInitiation))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageExitInitiation))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("exit initiation renders nondeterministically")
	}
	definition, _ := LookupPage(PageExitInitiation)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageExitInitiation {
		t.Fatal("exit initiation route does not round-trip")
	}
}

// Conformance: exit initiation keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_205_Conformance(t *testing.T) {
	if !PageVisible(PageExitInitiation, []string{"worker_self"}) {
		t.Fatal("exit initiation hidden from the employee")
	}
	if PageVisible(PageExitInitiation, nil) {
		t.Fatal("exit initiation visible without roles")
	}
	definition, _ := LookupPage(PageExitInitiation)
	if definition.PrimaryNav {
		t.Fatal("exit initiation claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageExitInitiation), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("exit initiation leaks a key in %s", code)
		}
	}
}
