package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-176: assigned learning. The registry owns
// every product surface, but no learning surface exists:
// assigned learning has no exposure point and the first
// surface invents assignment data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that assigns
// nothing until the governed learning service publishes,
// with assignment truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_176(t *testing.T) {
	definition, ok := LookupPage(PageAssignedLearning)
	if !ok {
		t.Fatal("assigned learning unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("assigned learning incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageAssignedLearning {
		t.Fatal("assigned learning route does not round-trip")
	}
	doc, err := Render(testView(PageAssignedLearning))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("assigned learning exposes an unresolved message key")
	}
	for _, invented := range []string{"assigned:", "course:", "due:", "complete ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("assigned learning invents assignment data: %q", invented)
		}
	}
}

// Golden: the registered assigned learning definition
// and its fallback copy.
func TestTodo_WEB_176_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageAssignedLearning)
	if !ok {
		t.Fatal("assigned learning unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("assigned_learning.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("assigned_learning.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "dde2ae9f3661a77ac7fca1f0494f5d89f160686898345b622f8bdadcdecfc0da"
	if got != want {
		t.Fatalf("assigned learning digest = %s, want %s", got, want)
	}
}

// Browser: assigned learning renders deterministically
// and round-trips its route.
func TestTodo_WEB_176_Browser(t *testing.T) {
	first, err := Render(testView(PageAssignedLearning))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageAssignedLearning))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("assigned learning renders nondeterministically")
	}
	definition, _ := LookupPage(PageAssignedLearning)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageAssignedLearning {
		t.Fatal("assigned learning route does not round-trip")
	}
}

// Conformance: assigned learning keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_176_Conformance(t *testing.T) {
	if !PageVisible(PageAssignedLearning, []string{"worker_self"}) {
		t.Fatal("assigned learning hidden from the employee")
	}
	if PageVisible(PageAssignedLearning, nil) {
		t.Fatal("assigned learning visible without roles")
	}
	definition, _ := LookupPage(PageAssignedLearning)
	if definition.PrimaryNav {
		t.Fatal("assigned learning claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageAssignedLearning), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("assigned learning leaks a key in %s", code)
		}
	}
}
