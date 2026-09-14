package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-143: onboarding task completion. The
// registry owns every product surface, but no task
// completion surface exists: completing onboarding
// tasks has no exposure point and the first surface
// invents task states or completions by convention.
// The compiler needs the registered surface — canonical
// identity, route, and an honest fallback that completes
// nothing until the governed service publishes, with
// completion staying server authority — so the surface
// resolves today without a second source of business
// authority.
func TestTodo_WEB_143(t *testing.T) {
	definition, ok := LookupPage(PageOnboardingTasks)
	if !ok {
		t.Fatal("onboarding task completion unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("onboarding task completion incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageOnboardingTasks {
		t.Fatal("onboarding task route does not round-trip")
	}
	doc, err := Render(testView(PageOnboardingTasks))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("onboarding task completion exposes an unresolved message key")
	}
	for _, invented := range []string{"completed:", "0 of", "checklist:", "done ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("onboarding task completion invents task data: %q", invented)
		}
	}
}

// Golden: the registered task completion definition and
// its fallback copy.
func TestTodo_WEB_143_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageOnboardingTasks)
	if !ok {
		t.Fatal("onboarding task completion unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("onboarding_tasks.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("onboarding_tasks.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "bb9afeebd00184d33aa053477de16bf454d3423f4b2d08122f7f2ec5b4b9924e"
	if got != want {
		t.Fatalf("onboarding task digest = %s, want %s", got, want)
	}
}

// Browser: onboarding task completion renders
// deterministically and round-trips its route.
func TestTodo_WEB_143_Browser(t *testing.T) {
	first, err := Render(testView(PageOnboardingTasks))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageOnboardingTasks))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("onboarding task completion renders nondeterministically")
	}
	definition, _ := LookupPage(PageOnboardingTasks)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageOnboardingTasks {
		t.Fatal("onboarding task route does not round-trip")
	}
}

// Conformance: onboarding task completion keeps the
// registry contract — visible to hiring roles, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_143_Conformance(t *testing.T) {
	if !PageVisible(PageOnboardingTasks, []string{"hiring_manager"}) {
		t.Fatal("onboarding task completion hidden from hiring managers")
	}
	if PageVisible(PageOnboardingTasks, nil) {
		t.Fatal("onboarding task completion visible without roles")
	}
	definition, _ := LookupPage(PageOnboardingTasks)
	if definition.PrimaryNav {
		t.Fatal("onboarding task completion claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageOnboardingTasks), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("onboarding task completion leaks a key in %s", code)
		}
	}
}
