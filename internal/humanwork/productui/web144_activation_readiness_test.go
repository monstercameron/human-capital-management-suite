package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-144: worker-activation readiness. The
// registry owns every product surface, but no activation
// readiness surface exists: proving a worker ready to
// activate has no exposure point and the first surface
// invents readiness signals by convention. The compiler
// needs the registered surface — canonical identity,
// route, and an honest fallback that activates nothing
// until the governed activation service publishes, with
// activation staying server authority — so the surface
// resolves today without a second source of business
// authority.
func TestTodo_WEB_144(t *testing.T) {
	definition, ok := LookupPage(PageActivationReadiness)
	if !ok {
		t.Fatal("worker-activation readiness unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("worker-activation readiness incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageActivationReadiness {
		t.Fatal("activation readiness route does not round-trip")
	}
	doc, err := Render(testView(PageActivationReadiness))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("activation readiness exposes an unresolved message key")
	}
	for _, invented := range []string{"ready:", "0 of", "checklist:", "activated ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("activation readiness invents readiness data: %q", invented)
		}
	}
}

// Golden: the registered activation readiness definition
// and its fallback copy.
func TestTodo_WEB_144_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageActivationReadiness)
	if !ok {
		t.Fatal("worker-activation readiness unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("activation_readiness.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("activation_readiness.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "ac01b4b6fdc6e6fcb407ad97ae5a001224c04e724b2d634c33ccbcc0d9c5c8e0"
	if got != want {
		t.Fatalf("activation readiness digest = %s, want %s", got, want)
	}
}

// Browser: activation readiness renders deterministically
// and round-trips its route.
func TestTodo_WEB_144_Browser(t *testing.T) {
	first, err := Render(testView(PageActivationReadiness))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageActivationReadiness))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("activation readiness renders nondeterministically")
	}
	definition, _ := LookupPage(PageActivationReadiness)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageActivationReadiness {
		t.Fatal("activation readiness route does not round-trip")
	}
}

// Conformance: activation readiness keeps the registry
// contract — visible to hiring roles, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_144_Conformance(t *testing.T) {
	if !PageVisible(PageActivationReadiness, []string{"hiring_manager"}) {
		t.Fatal("activation readiness hidden from hiring managers")
	}
	if PageVisible(PageActivationReadiness, nil) {
		t.Fatal("activation readiness visible without roles")
	}
	definition, _ := LookupPage(PageActivationReadiness)
	if definition.PrimaryNav {
		t.Fatal("activation readiness claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageActivationReadiness), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("activation readiness leaks a key in %s", code)
		}
	}
}
