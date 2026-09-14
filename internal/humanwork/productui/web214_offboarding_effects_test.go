package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-214: offboarding external-effect status.
// The registry owns every product surface, but no
// external-effect status exists: showing payroll, access,
// and benefit cutoffs of an exit has no exposure point
// and the first surface invents effect data by
// convention. The compiler needs the registered surface
// — canonical identity, route, and an honest fallback
// that shows nothing until the governed lifecycle
// service publishes, with effect truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_214(t *testing.T) {
	definition, ok := LookupPage(PageOffboardingEffects)
	if !ok {
		t.Fatal("offboarding external-effect status unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("offboarding external-effect status incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageOffboardingEffects {
		t.Fatal("offboarding external-effect status route does not round-trip")
	}
	doc, err := Render(testView(PageOffboardingEffects))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("offboarding external-effect status exposes an unresolved message key")
	}
	for _, invented := range []string{"payroll cut off", "benefits end Sep 30", "accounts revoked", "effects ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("offboarding external-effect status invents effect data: %q", invented)
		}
	}
}

// Golden: the registered external-effect definition
// and its fallback copy.
func TestTodo_WEB_214_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageOffboardingEffects)
	if !ok {
		t.Fatal("offboarding external-effect status unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("offboarding_effects.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("offboarding_effects.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "8f321fcf1717fb2edceabd34edb9d8ff8cd92ca87582e7f8a38b6870391641b7"
	if got != want {
		t.Fatalf("offboarding external-effect status digest = %s, want %s", got, want)
	}
}

// Browser: offboarding external-effect status renders
// deterministically and round-trips its route.
func TestTodo_WEB_214_Browser(t *testing.T) {
	first, err := Render(testView(PageOffboardingEffects))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageOffboardingEffects))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("offboarding external-effect status renders nondeterministically")
	}
	definition, _ := LookupPage(PageOffboardingEffects)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageOffboardingEffects {
		t.Fatal("offboarding external-effect status route does not round-trip")
	}
}

// Conformance: offboarding external-effect status keeps
// the registry contract — visible to the manager,
// hidden from the role-less baseline, ordered, and
// honest in every locale.
func TestTodo_WEB_214_Conformance(t *testing.T) {
	if !PageVisible(PageOffboardingEffects, []string{"manager"}) {
		t.Fatal("offboarding external-effect status hidden from the manager")
	}
	if PageVisible(PageOffboardingEffects, nil) {
		t.Fatal("offboarding external-effect status visible without roles")
	}
	definition, _ := LookupPage(PageOffboardingEffects)
	if definition.PrimaryNav {
		t.Fatal("offboarding external-effect status claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageOffboardingEffects), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("offboarding external-effect status leaks a key in %s", code)
		}
	}
}
