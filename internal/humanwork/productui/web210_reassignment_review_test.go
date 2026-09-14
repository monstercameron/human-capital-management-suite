package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-210: manager and work reassignment
// review. The registry owns every product surface, but
// no reassignment review exists: reviewing manager and
// work reassignment has no exposure point and the first
// surface invents reassignment data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that reviews
// nothing until the governed lifecycle service
// publishes, with reassignment truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_210(t *testing.T) {
	definition, ok := LookupPage(PageReassignmentReview)
	if !ok {
		t.Fatal("manager and work reassignment review unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("manager and work reassignment review incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageReassignmentReview {
		t.Fatal("manager and work reassignment review route does not round-trip")
	}
	doc, err := Render(testView(PageReassignmentReview))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("manager and work reassignment review exposes an unresolved message key")
	}
	for _, invented := range []string{"reports move to Jo", "12 items reassigned", "reviewed by Ash", "reassignment ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("manager and work reassignment review invents reassignment data: %q", invented)
		}
	}
}

// Golden: the registered reassignment definition and
// its fallback copy.
func TestTodo_WEB_210_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageReassignmentReview)
	if !ok {
		t.Fatal("manager and work reassignment review unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("reassignment_review.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("reassignment_review.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "d2d4b205e620c2929fc69cbb4d833e8d64c6e403279f85860aee859daec4cf69"
	if got != want {
		t.Fatalf("manager and work reassignment review digest = %s, want %s", got, want)
	}
}

// Browser: manager and work reassignment review renders
// deterministically and round-trips its route.
func TestTodo_WEB_210_Browser(t *testing.T) {
	first, err := Render(testView(PageReassignmentReview))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageReassignmentReview))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("manager and work reassignment review renders nondeterministically")
	}
	definition, _ := LookupPage(PageReassignmentReview)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageReassignmentReview {
		t.Fatal("manager and work reassignment review route does not round-trip")
	}
}

// Conformance: manager and work reassignment review
// keeps the registry contract — visible to the manager,
// hidden from the role-less baseline, ordered, and
// honest in every locale.
func TestTodo_WEB_210_Conformance(t *testing.T) {
	if !PageVisible(PageReassignmentReview, []string{"manager"}) {
		t.Fatal("manager and work reassignment review hidden from the manager")
	}
	if PageVisible(PageReassignmentReview, nil) {
		t.Fatal("manager and work reassignment review visible without roles")
	}
	definition, _ := LookupPage(PageReassignmentReview)
	if definition.PrimaryNav {
		t.Fatal("manager and work reassignment review claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageReassignmentReview), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("manager and work reassignment review leaks a key in %s", code)
		}
	}
}
