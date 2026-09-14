package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-202: case finding and disposition. The
// registry owns every product surface, but no finding
// surface exists: recording case findings and their
// disposition has no exposure point and the first
// surface invents finding data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that finds
// nothing until the governed help service publishes,
// with finding truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_202(t *testing.T) {
	definition, ok := LookupPage(PageCaseDisposition)
	if !ok {
		t.Fatal("case finding and disposition unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("case finding and disposition incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageCaseDisposition {
		t.Fatal("case finding and disposition route does not round-trip")
	}
	doc, err := Render(testView(PageCaseDisposition))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("case finding and disposition exposes an unresolved message key")
	}
	for _, invented := range []string{"policy breach found", "closed with warning", "decided by Rami", "finding ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("case finding and disposition invents finding data: %q", invented)
		}
	}
}

// Golden: the registered finding definition and its
// fallback copy.
func TestTodo_WEB_202_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageCaseDisposition)
	if !ok {
		t.Fatal("case finding and disposition unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("case_disposition.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("case_disposition.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "b3e901e28ec7a9d3b5a2e693fa4f534add3ea9c61f9aed3c12169f91af9b2433"
	if got != want {
		t.Fatalf("case finding and disposition digest = %s, want %s", got, want)
	}
}

// Browser: case finding and disposition renders
// deterministically and round-trips its route.
func TestTodo_WEB_202_Browser(t *testing.T) {
	first, err := Render(testView(PageCaseDisposition))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageCaseDisposition))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("case finding and disposition renders nondeterministically")
	}
	definition, _ := LookupPage(PageCaseDisposition)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageCaseDisposition {
		t.Fatal("case finding and disposition route does not round-trip")
	}
}

// Conformance: case finding and disposition keeps the
// registry contract — visible to the HR specialist,
// hidden from the role-less baseline, ordered, and
// honest in every locale.
func TestTodo_WEB_202_Conformance(t *testing.T) {
	if !PageVisible(PageCaseDisposition, []string{"hr_partner"}) {
		t.Fatal("case finding and disposition hidden from the HR specialist")
	}
	if PageVisible(PageCaseDisposition, nil) {
		t.Fatal("case finding and disposition visible without roles")
	}
	definition, _ := LookupPage(PageCaseDisposition)
	if definition.PrimaryNav {
		t.Fatal("case finding and disposition claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageCaseDisposition), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("case finding and disposition leaks a key in %s", code)
		}
	}
}
