package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-193: the employee Help hub. The registry
// owns every product surface, but no Help hub exists:
// reaching employee help has no exposure point and the
// first surface invents help data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that helps
// with nothing until the governed help service
// publishes, with help truth staying server authority —
// so the surface resolves today without a second source
// of business authority.
func TestTodo_WEB_193(t *testing.T) {
	definition, ok := LookupPage(PageHelpHub)
	if !ok {
		t.Fatal("employee Help hub unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("employee Help hub incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageHelpHub {
		t.Fatal("employee Help hub route does not round-trip")
	}
	doc, err := Render(testView(PageHelpHub))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("employee Help hub exposes an unresolved message key")
	}
	for _, invented := range []string{"top article: PTO", "answered by June", "ticket 8811 open", "help ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("employee Help hub invents help data: %q", invented)
		}
	}
}

// Golden: the registered Help hub definition and its
// fallback copy.
func TestTodo_WEB_193_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageHelpHub)
	if !ok {
		t.Fatal("employee Help hub unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("help_hub.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("help_hub.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "9614c18d0d1136404e4d25e6c3bc41e70a56fb88c2a81d93543ee26a60625f7c"
	if got != want {
		t.Fatalf("employee Help hub digest = %s, want %s", got, want)
	}
}

// Browser: employee Help hub renders deterministically
// and round-trips its route.
func TestTodo_WEB_193_Browser(t *testing.T) {
	first, err := Render(testView(PageHelpHub))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageHelpHub))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("employee Help hub renders nondeterministically")
	}
	definition, _ := LookupPage(PageHelpHub)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageHelpHub {
		t.Fatal("employee Help hub route does not round-trip")
	}
}

// Conformance: employee Help hub keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_193_Conformance(t *testing.T) {
	if !PageVisible(PageHelpHub, []string{"worker_self"}) {
		t.Fatal("employee Help hub hidden from the employee")
	}
	if PageVisible(PageHelpHub, nil) {
		t.Fatal("employee Help hub visible without roles")
	}
	definition, _ := LookupPage(PageHelpHub)
	if definition.PrimaryNav {
		t.Fatal("employee Help hub claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageHelpHub), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("employee Help hub leaks a key in %s", code)
		}
	}
}
