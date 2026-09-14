package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-209: the offboarding plan. The registry
// owns every product surface, but no offboarding plan
// exists: planning a worker exit has no exposure point
// and the first surface invents plan data by convention.
// The compiler needs the registered surface — canonical
// identity, route, and an honest fallback that plans
// nothing until the governed lifecycle service
// publishes, with plan truth staying server authority —
// so the surface resolves today without a second source
// of business authority.
func TestTodo_WEB_209(t *testing.T) {
	definition, ok := LookupPage(PageOffboardingPlan)
	if !ok {
		t.Fatal("offboarding plan unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("offboarding plan incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageOffboardingPlan {
		t.Fatal("offboarding plan route does not round-trip")
	}
	doc, err := Render(testView(PageOffboardingPlan))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("offboarding plan exposes an unresolved message key")
	}
	for _, invented := range []string{"7 tasks assigned", "handover to Sam", "due before Friday", "plan ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("offboarding plan invents plan data: %q", invented)
		}
	}
}

// Golden: the registered offboarding plan definition
// and its fallback copy.
func TestTodo_WEB_209_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageOffboardingPlan)
	if !ok {
		t.Fatal("offboarding plan unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("offboarding_plan.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("offboarding_plan.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "ee4fb3cf6aa33503874a3d5181fd3dbaf8fb64ac03aa41ce2bc47f18d58691ee"
	if got != want {
		t.Fatalf("offboarding plan digest = %s, want %s", got, want)
	}
}

// Browser: offboarding plan renders deterministically
// and round-trips its route.
func TestTodo_WEB_209_Browser(t *testing.T) {
	first, err := Render(testView(PageOffboardingPlan))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageOffboardingPlan))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("offboarding plan renders nondeterministically")
	}
	definition, _ := LookupPage(PageOffboardingPlan)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageOffboardingPlan {
		t.Fatal("offboarding plan route does not round-trip")
	}
}

// Conformance: offboarding plan keeps the registry
// contract — visible to the manager, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_209_Conformance(t *testing.T) {
	if !PageVisible(PageOffboardingPlan, []string{"manager"}) {
		t.Fatal("offboarding plan hidden from the manager")
	}
	if PageVisible(PageOffboardingPlan, nil) {
		t.Fatal("offboarding plan visible without roles")
	}
	definition, _ := LookupPage(PageOffboardingPlan)
	if definition.PrimaryNav {
		t.Fatal("offboarding plan claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageOffboardingPlan), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("offboarding plan leaks a key in %s", code)
		}
	}
}
