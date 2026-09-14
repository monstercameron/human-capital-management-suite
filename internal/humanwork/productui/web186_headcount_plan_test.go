package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-186: the headcount-plan workspace. The
// registry owns every product surface, but no headcount
// plan exists: planning headcount has no exposure point
// and the first surface invents plan data by convention.
// The compiler needs the registered surface — canonical
// identity, route, and an honest fallback that plans
// nothing until the governed headcount service
// publishes, with plan truth staying server authority —
// so the surface resolves today without a second source
// of business authority.
func TestTodo_WEB_186(t *testing.T) {
	definition, ok := LookupPage(PageHeadcountPlan)
	if !ok {
		t.Fatal("headcount-plan workspace unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("headcount-plan workspace incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageHeadcountPlan {
		t.Fatal("headcount-plan workspace route does not round-trip")
	}
	doc, err := Render(testView(PageHeadcountPlan))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("headcount-plan workspace exposes an unresolved message key")
	}
	for _, invented := range []string{"plan FY27 draft", "412 approved heads", "owned by Priya", "headcount ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("headcount-plan workspace invents plan data: %q", invented)
		}
	}
}

// Golden: the registered headcount-plan definition and
// its fallback copy.
func TestTodo_WEB_186_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageHeadcountPlan)
	if !ok {
		t.Fatal("headcount-plan workspace unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("headcount_plan.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("headcount_plan.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "08df37f4f3fe4287edd1dab4f7f3ea246b85b902f49d3205f87d24d079c441fd"
	if got != want {
		t.Fatalf("headcount-plan digest = %s, want %s", got, want)
	}
}

// Browser: headcount-plan workspace renders
// deterministically and round-trips its route.
func TestTodo_WEB_186_Browser(t *testing.T) {
	first, err := Render(testView(PageHeadcountPlan))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageHeadcountPlan))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("headcount-plan workspace renders nondeterministically")
	}
	definition, _ := LookupPage(PageHeadcountPlan)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageHeadcountPlan {
		t.Fatal("headcount-plan workspace route does not round-trip")
	}
}

// Conformance: headcount-plan workspace keeps the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_186_Conformance(t *testing.T) {
	if !PageVisible(PageHeadcountPlan, []string{"worker_self"}) {
		t.Fatal("headcount-plan workspace hidden from the employee")
	}
	if PageVisible(PageHeadcountPlan, nil) {
		t.Fatal("headcount-plan workspace visible without roles")
	}
	definition, _ := LookupPage(PageHeadcountPlan)
	if definition.PrimaryNav {
		t.Fatal("headcount-plan workspace claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageHeadcountPlan), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("headcount-plan workspace leaks a key in %s", code)
		}
	}
}
