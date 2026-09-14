package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-167: benefit enrollment. The registry owns
// every product surface, but no enrollment surface
// exists: enrolling in benefits has no exposure point and
// the first surface invents enrollment state by
// convention. The compiler needs the registered surface —
// canonical identity, route, and an honest fallback that
// enrolls nothing until the governed benefits service
// publishes, with enrollment truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_167(t *testing.T) {
	definition, ok := LookupPage(PageBenefitsEnroll)
	if !ok {
		t.Fatal("benefit enrollment unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("benefit enrollment incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageBenefitsEnroll {
		t.Fatal("benefits enrollment route does not round-trip")
	}
	doc, err := Render(testView(PageBenefitsEnroll))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("benefits enrollment exposes an unresolved message key")
	}
	for _, invented := range []string{"enrolled:", "election:", "confirmed:", "done ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("benefits enrollment invents enrollment data: %q", invented)
		}
	}
}

// Golden: the registered benefits enrollment definition
// and its fallback copy.
func TestTodo_WEB_167_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageBenefitsEnroll)
	if !ok {
		t.Fatal("benefit enrollment unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("benefits_enroll.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("benefits_enroll.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "73c7b3061a5955e57464c9fc346e38a8d2999b30b4f5b7bcfff8231889e7befc"
	if got != want {
		t.Fatalf("benefits enrollment digest = %s, want %s", got, want)
	}
}

// Browser: benefits enrollment renders deterministically
// and round-trips its route.
func TestTodo_WEB_167_Browser(t *testing.T) {
	first, err := Render(testView(PageBenefitsEnroll))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageBenefitsEnroll))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("benefits enrollment renders nondeterministically")
	}
	definition, _ := LookupPage(PageBenefitsEnroll)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageBenefitsEnroll {
		t.Fatal("benefits enrollment route does not round-trip")
	}
}

// Conformance: benefits enrollment keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_167_Conformance(t *testing.T) {
	if !PageVisible(PageBenefitsEnroll, []string{"worker_self"}) {
		t.Fatal("benefits enrollment hidden from the employee")
	}
	if PageVisible(PageBenefitsEnroll, nil) {
		t.Fatal("benefits enrollment visible without roles")
	}
	definition, _ := LookupPage(PageBenefitsEnroll)
	if definition.PrimaryNav {
		t.Fatal("benefits enrollment claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageBenefitsEnroll), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("benefits enrollment leaks a key in %s", code)
		}
	}
}
