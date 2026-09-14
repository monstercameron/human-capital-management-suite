package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-191: distinguishing planned state from
// committed truth. The registry owns every product
// surface, but no planned-versus-committed distinction
// exists: showing what is planned against what is
// committed has no exposure point and the first surface
// invents the distinction by convention. The compiler
// needs the registered surface — canonical identity,
// route, and an honest fallback that distinguishes
// nothing until the governed organization service
// publishes, with plan and commitment truth staying
// server authority — so the surface resolves today
// without a second source of business authority.
func TestTodo_WEB_191(t *testing.T) {
	definition, ok := LookupPage(PagePlannedVsCommitted)
	if !ok {
		t.Fatal("planned-versus-committed distinction unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("planned-versus-committed distinction incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PagePlannedVsCommitted {
		t.Fatal("planned-versus-committed route does not round-trip")
	}
	doc, err := Render(testView(PagePlannedVsCommitted))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("planned-versus-committed exposes an unresolved message key")
	}
	for _, invented := range []string{"3 planned, 9 committed", "draft awaiting signoff", "committed by Ilya", "planned ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("planned-versus-committed invents distinction data: %q", invented)
		}
	}
}

// Golden: the registered planned-versus-committed
// definition and its fallback copy.
func TestTodo_WEB_191_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PagePlannedVsCommitted)
	if !ok {
		t.Fatal("planned-versus-committed distinction unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("planned_committed.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("planned_committed.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "e872e0785464ba95040e9ba2469db4af7a16abcfcc9c8e72bd006b086ef6a500"
	if got != want {
		t.Fatalf("planned-versus-committed digest = %s, want %s", got, want)
	}
}

// Browser: planned-versus-committed renders
// deterministically and round-trips its route.
func TestTodo_WEB_191_Browser(t *testing.T) {
	first, err := Render(testView(PagePlannedVsCommitted))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PagePlannedVsCommitted))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("planned-versus-committed renders nondeterministically")
	}
	definition, _ := LookupPage(PagePlannedVsCommitted)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PagePlannedVsCommitted {
		t.Fatal("planned-versus-committed route does not round-trip")
	}
}

// Conformance: planned-versus-committed keeps the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_191_Conformance(t *testing.T) {
	if !PageVisible(PagePlannedVsCommitted, []string{"worker_self"}) {
		t.Fatal("planned-versus-committed hidden from the employee")
	}
	if PageVisible(PagePlannedVsCommitted, nil) {
		t.Fatal("planned-versus-committed visible without roles")
	}
	definition, _ := LookupPage(PagePlannedVsCommitted)
	if definition.PrimaryNav {
		t.Fatal("planned-versus-committed claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PagePlannedVsCommitted), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("planned-versus-committed leaks a key in %s", code)
		}
	}
}
