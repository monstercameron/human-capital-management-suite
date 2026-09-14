package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-142: the onboarding-plan page. The
// registry owns every product surface, but no
// onboarding page exists: onboarding plans have no
// exposure point and the first surface invents tasks,
// owners, or due dates by convention. The compiler needs
// the registered page — canonical identity, route, and
// an honest fallback that exposes no plan until the
// governed service publishes — so the page resolves
// today without a second source of business authority.
func TestTodo_WEB_142(t *testing.T) {
	definition, ok := LookupPage(PageOnboarding)
	if !ok {
		t.Fatal("onboarding-plan page unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("onboarding-plan page incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageOnboarding {
		t.Fatal("onboarding route does not round-trip")
	}
	doc, err := Render(testView(PageOnboarding))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("onboarding-plan page exposes an unresolved message key")
	}
	for _, invented := range []string{"Day 1", "task:", "owner:", "due:"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("onboarding-plan page invents plan data: %q", invented)
		}
	}
}

// Golden: the registered onboarding definition and its
// fallback copy.
func TestTodo_WEB_142_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageOnboarding)
	if !ok {
		t.Fatal("onboarding-plan page unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("onboarding.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("onboarding.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "40fbde3af1598f0594638e7957d19c203b331323a3f113c60d625c3e5217c9ea"
	if got != want {
		t.Fatalf("onboarding digest = %s, want %s", got, want)
	}
}

// Browser: the onboarding-plan page renders
// deterministically and round-trips its route.
func TestTodo_WEB_142_Browser(t *testing.T) {
	first, err := Render(testView(PageOnboarding))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageOnboarding))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("onboarding-plan page renders nondeterministically")
	}
	definition, _ := LookupPage(PageOnboarding)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageOnboarding {
		t.Fatal("onboarding route does not round-trip")
	}
}

// Conformance: the onboarding-plan page keeps the
// registry contract — visible to hiring roles, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_142_Conformance(t *testing.T) {
	if !PageVisible(PageOnboarding, []string{"hiring_manager"}) {
		t.Fatal("onboarding-plan page hidden from hiring managers")
	}
	if PageVisible(PageOnboarding, nil) {
		t.Fatal("onboarding-plan page visible without roles")
	}
	definition, _ := LookupPage(PageOnboarding)
	if definition.PrimaryNav {
		t.Fatal("onboarding-plan page claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageOnboarding), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("onboarding-plan page leaks a key in %s", code)
		}
	}
}
