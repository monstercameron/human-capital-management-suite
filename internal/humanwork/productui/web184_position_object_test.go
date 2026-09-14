package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-184: the position object page. The
// registry owns every product surface, but no position
// object exists: viewing a governed position has no
// exposure point and the first surface invents position
// data by convention. The compiler needs the registered
// surface — canonical identity, route, and an honest
// fallback that shows nothing until the governed
// position service publishes, with position truth
// staying server authority — so the surface resolves
// today without a second source of business authority.
func TestTodo_WEB_184(t *testing.T) {
	definition, ok := LookupPage(PagePositionObject)
	if !ok {
		t.Fatal("position object page unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("position object page incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PagePositionObject {
		t.Fatal("position object route does not round-trip")
	}
	doc, err := Render(testView(PagePositionObject))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("position object page exposes an unresolved message key")
	}
	for _, invented := range []string{"senior engineer IV", "req 5521", "vacant since March", "position ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("position object page invents position data: %q", invented)
		}
	}
}

// Golden: the registered position object definition
// and its fallback copy.
func TestTodo_WEB_184_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PagePositionObject)
	if !ok {
		t.Fatal("position object page unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("position_object.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("position_object.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "a3a62dc9d911532e544c91778973e71f42231ea09fee18ca7893f84f0d6342a1"
	if got != want {
		t.Fatalf("position object digest = %s, want %s", got, want)
	}
}

// Browser: position object page renders
// deterministically and round-trips its route.
func TestTodo_WEB_184_Browser(t *testing.T) {
	first, err := Render(testView(PagePositionObject))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PagePositionObject))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("position object page renders nondeterministically")
	}
	definition, _ := LookupPage(PagePositionObject)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PagePositionObject {
		t.Fatal("position object route does not round-trip")
	}
}

// Conformance: position object page keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_184_Conformance(t *testing.T) {
	if !PageVisible(PagePositionObject, []string{"worker_self"}) {
		t.Fatal("position object page hidden from the employee")
	}
	if PageVisible(PagePositionObject, nil) {
		t.Fatal("position object page visible without roles")
	}
	definition, _ := LookupPage(PagePositionObject)
	if definition.PrimaryNav {
		t.Fatal("position object page claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PagePositionObject), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("position object page leaks a key in %s", code)
		}
	}
}
