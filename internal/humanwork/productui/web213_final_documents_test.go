package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-213: final-document delivery. The
// registry owns every product surface, but no final
// delivery exists: delivering final documents has no
// exposure point and the first surface invents delivery
// data by convention. The compiler needs the registered
// surface — canonical identity, route, and an honest
// fallback that delivers nothing until the governed
// lifecycle service publishes, with document truth
// staying server authority — so the surface resolves
// today without a second source of business authority.
func TestTodo_WEB_213(t *testing.T) {
	definition, ok := LookupPage(PageFinalDocuments)
	if !ok {
		t.Fatal("final-document delivery unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("final-document delivery incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageFinalDocuments {
		t.Fatal("final-document delivery route does not round-trip")
	}
	doc, err := Render(testView(PageFinalDocuments))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("final-document delivery exposes an unresolved message key")
	}
	for _, invented := range []string{"experience letter ready", "final payslip issued", "download 4 files", "delivery ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("final-document delivery invents delivery data: %q", invented)
		}
	}
}

// Golden: the registered final delivery definition
// and its fallback copy.
func TestTodo_WEB_213_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageFinalDocuments)
	if !ok {
		t.Fatal("final-document delivery unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("final_documents.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("final_documents.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "8b25ac0593e1a865d9494d6642334280088b5de0d09bb2646e9058aae9695120"
	if got != want {
		t.Fatalf("final-document delivery digest = %s, want %s", got, want)
	}
}

// Browser: final-document delivery renders
// deterministically and round-trips its route.
func TestTodo_WEB_213_Browser(t *testing.T) {
	first, err := Render(testView(PageFinalDocuments))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageFinalDocuments))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("final-document delivery renders nondeterministically")
	}
	definition, _ := LookupPage(PageFinalDocuments)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageFinalDocuments {
		t.Fatal("final-document delivery route does not round-trip")
	}
}

// Conformance: final-document delivery keeps the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_213_Conformance(t *testing.T) {
	if !PageVisible(PageFinalDocuments, []string{"worker_self"}) {
		t.Fatal("final-document delivery hidden from the employee")
	}
	if PageVisible(PageFinalDocuments, nil) {
		t.Fatal("final-document delivery visible without roles")
	}
	definition, _ := LookupPage(PageFinalDocuments)
	if definition.PrimaryNav {
		t.Fatal("final-document delivery claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageFinalDocuments), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("final-document delivery leaks a key in %s", code)
		}
	}
}
