package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-135: the requisition workspace. The
// registry owns every product surface, but no
// requisition workspace exists: the requisition
// lifecycle (RECRUIT-001) has no exposure point and the
// first surface invents requisition data by convention.
// The compiler needs the registered page — canonical
// identity, route, and an honest fallback that exposes
// no requisitions until the governed service publishes —
// so the workspace resolves today without a second
// source of business authority.
func TestTodo_WEB_135(t *testing.T) {
	definition, ok := LookupPage(PageRequisition)
	if !ok {
		t.Fatal("requisition workspace unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("requisition workspace incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageRequisition {
		t.Fatal("requisition route does not round-trip")
	}
	doc, err := Render(testView(PageRequisition))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("requisition workspace exposes an unresolved message key")
	}
	for _, invented := range []string{"REQ-", "requisition #", "candidates", "interviews:"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("requisition workspace invents requisition data: %q", invented)
		}
	}
}

// Golden: the registered requisition definition and its
// fallback copy.
func TestTodo_WEB_135_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageRequisition)
	if !ok {
		t.Fatal("requisition workspace unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("requisition.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("requisition.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "2d8852e26b7047504f6e41a2445cae4d625f8fe1ceea12e7720fa8ebcac6f8bc"
	if got != want {
		t.Fatalf("requisition digest = %s, want %s", got, want)
	}
}

// Browser: the requisition workspace renders
// deterministically and round-trips its route.
func TestTodo_WEB_135_Browser(t *testing.T) {
	first, err := Render(testView(PageRequisition))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageRequisition))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("requisition workspace renders nondeterministically")
	}
	definition, _ := LookupPage(PageRequisition)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageRequisition {
		t.Fatal("requisition route does not round-trip")
	}
}

// Conformance: the requisition workspace keeps the
// registry contract — visible to hiring roles, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_135_Conformance(t *testing.T) {
	if !PageVisible(PageRequisition, []string{"hiring_manager"}) {
		t.Fatal("requisition workspace hidden from hiring managers")
	}
	if PageVisible(PageRequisition, nil) {
		t.Fatal("requisition workspace visible without roles")
	}
	definition, _ := LookupPage(PageRequisition)
	if definition.PrimaryNav {
		t.Fatal("requisition workspace claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageRequisition), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("requisition workspace leaks a key in %s", code)
		}
	}
}
