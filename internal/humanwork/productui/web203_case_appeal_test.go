package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-203: case appeal. The registry owns every
// product surface, but no appeal surface exists: appealing
// a case finding has no exposure point and the first
// surface invents appeal data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that appeals
// nothing until the governed help service publishes,
// with appeal truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_203(t *testing.T) {
	definition, ok := LookupPage(PageCaseAppeal)
	if !ok {
		t.Fatal("case appeal unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("case appeal incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageCaseAppeal {
		t.Fatal("case appeal route does not round-trip")
	}
	doc, err := Render(testView(PageCaseAppeal))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("case appeal exposes an unresolved message key")
	}
	for _, invented := range []string{"appeal filed today", "reviewer is Paz", "overturned on review", "appeal ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("case appeal invents appeal data: %q", invented)
		}
	}
}

// Golden: the registered appeal definition and its
// fallback copy.
func TestTodo_WEB_203_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageCaseAppeal)
	if !ok {
		t.Fatal("case appeal unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("case_appeal.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("case_appeal.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "fb0085d8abf0c973d978da540fec0bd6ff54ab9c9265d7582f4e97ad1c6d66da"
	if got != want {
		t.Fatalf("case appeal digest = %s, want %s", got, want)
	}
}

// Browser: case appeal renders deterministically and
// round-trips its route.
func TestTodo_WEB_203_Browser(t *testing.T) {
	first, err := Render(testView(PageCaseAppeal))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageCaseAppeal))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("case appeal renders nondeterministically")
	}
	definition, _ := LookupPage(PageCaseAppeal)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageCaseAppeal {
		t.Fatal("case appeal route does not round-trip")
	}
}

// Conformance: case appeal keeps the registry contract
// — visible to the employee, hidden from the role-less
// baseline, ordered, and honest in every locale.
func TestTodo_WEB_203_Conformance(t *testing.T) {
	if !PageVisible(PageCaseAppeal, []string{"worker_self"}) {
		t.Fatal("case appeal hidden from the employee")
	}
	if PageVisible(PageCaseAppeal, nil) {
		t.Fatal("case appeal visible without roles")
	}
	definition, _ := LookupPage(PageCaseAppeal)
	if definition.PrimaryNav {
		t.Fatal("case appeal claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageCaseAppeal), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("case appeal leaks a key in %s", code)
		}
	}
}
