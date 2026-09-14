package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-212: access and equipment reconciliation.
// The registry owns every product surface, but no access
// reconciliation exists: reconciling access and equipment
// return has no exposure point and the first surface
// invents reconciliation data by convention. The compiler
// needs the registered surface — canonical identity,
// route, and an honest fallback that reconciles nothing
// until the governed lifecycle service publishes, with
// reconciliation truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_212(t *testing.T) {
	definition, ok := LookupPage(PageAccessEquipment)
	if !ok {
		t.Fatal("access and equipment reconciliation unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("access and equipment reconciliation incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageAccessEquipment {
		t.Fatal("access and equipment reconciliation route does not round-trip")
	}
	doc, err := Render(testView(PageAccessEquipment))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("access and equipment reconciliation exposes an unresolved message key")
	}
	for _, invented := range []string{"laptop returned", "badge deactivated", "2 accounts open", "reconciled ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("access and equipment reconciliation invents reconciliation data: %q", invented)
		}
	}
}

// Golden: the registered reconciliation definition
// and its fallback copy.
func TestTodo_WEB_212_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageAccessEquipment)
	if !ok {
		t.Fatal("access and equipment reconciliation unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("access_equipment.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("access_equipment.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "03fde184948ea367e993f1debf12b093739ee902ebc682e70758bad6d76f8f57"
	if got != want {
		t.Fatalf("access and equipment reconciliation digest = %s, want %s", got, want)
	}
}

// Browser: access and equipment reconciliation renders
// deterministically and round-trips its route.
func TestTodo_WEB_212_Browser(t *testing.T) {
	first, err := Render(testView(PageAccessEquipment))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageAccessEquipment))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("access and equipment reconciliation renders nondeterministically")
	}
	definition, _ := LookupPage(PageAccessEquipment)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageAccessEquipment {
		t.Fatal("access and equipment reconciliation route does not round-trip")
	}
}

// Conformance: access and equipment reconciliation
// keeps the registry contract — visible to the manager,
// hidden from the role-less baseline, ordered, and
// honest in every locale.
func TestTodo_WEB_212_Conformance(t *testing.T) {
	if !PageVisible(PageAccessEquipment, []string{"manager"}) {
		t.Fatal("access and equipment reconciliation hidden from the manager")
	}
	if PageVisible(PageAccessEquipment, nil) {
		t.Fatal("access and equipment reconciliation visible without roles")
	}
	definition, _ := LookupPage(PageAccessEquipment)
	if definition.PrimaryNav {
		t.Fatal("access and equipment reconciliation claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageAccessEquipment), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("access and equipment reconciliation leaks a key in %s", code)
		}
	}
}
