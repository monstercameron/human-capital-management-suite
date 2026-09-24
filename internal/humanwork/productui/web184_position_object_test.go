package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// WEB-184 keeps the position object route and presentation contract stable.
// Position facts are rendered only from the server-authorized projection.
func TestTodo_WEB_184(t *testing.T) {
	definition, ok := LookupPage(PagePositionObject)
	if !ok {
		t.Fatal("position object page unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
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

// Golden: the registered position object definition and its selection copy.
func TestTodo_WEB_184_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PagePositionObject)
	if !ok {
		t.Fatal("position object page unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("position_object.select_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("position_object.select_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	// Re-pinned 2026-09-24 after reviewing the position selection copy.
	const want = "e997e758a67ed9f28c649813b3f152ecb13299594df4ebf583edc5393d16aa7e"
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

// Conformance: the route remains role-scoped and its selection copy resolves
// in every supported locale.
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
