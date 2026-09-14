package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-133: the headcount-request page. The
// registry owns every product surface, but no headcount
// page exists: the requisition lifecycle (RECRUIT-001)
// has no exposure point and the first surface invents
// requisition data by convention. The compiler needs the
// registered page — canonical identity, route, and an
// honest fallback that exposes no requisitions until the
// governed service publishes — so the page resolves
// today without a second source of business authority.
func TestTodo_WEB_133(t *testing.T) {
	definition, ok := LookupPage(PageHeadcount)
	if !ok {
		t.Fatal("headcount page unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("headcount page incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageHeadcount {
		t.Fatal("headcount route does not round-trip")
	}
	doc, err := Render(testView(PageHeadcount))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("headcount page exposes an unresolved message key")
	}
	for _, invented := range []string{"REQ-", "requisition #", "candidates"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("headcount page invents requisition data: %q", invented)
		}
	}
}

// Golden: the registered headcount definition and its
// fallback copy.
func TestTodo_WEB_133_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageHeadcount)
	if !ok {
		t.Fatal("headcount page unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("headcount.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("headcount.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "511fc3b8d5cd610fedb36b97c2895045886f8d7bfe79c0553b46212118e38888"
	if got != want {
		t.Fatalf("headcount digest = %s, want %s", got, want)
	}
}

// Browser: the headcount page renders deterministically
// and round-trips its route.
func TestTodo_WEB_133_Browser(t *testing.T) {
	first, err := Render(testView(PageHeadcount))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageHeadcount))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("headcount page renders nondeterministically")
	}
	definition, _ := LookupPage(PageHeadcount)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageHeadcount {
		t.Fatal("headcount route does not round-trip")
	}
}

// Conformance: the headcount page keeps the registry
// contract — visible to hiring roles, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_133_Conformance(t *testing.T) {
	if !PageVisible(PageHeadcount, []string{"hiring_manager"}) {
		t.Fatal("headcount page hidden from hiring managers")
	}
	if PageVisible(PageHeadcount, nil) {
		t.Fatal("headcount page visible without roles")
	}
	definition, _ := LookupPage(PageHeadcount)
	if definition.PrimaryNav {
		t.Fatal("headcount page claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageHeadcount), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("headcount page leaks a key in %s", code)
		}
	}
}
