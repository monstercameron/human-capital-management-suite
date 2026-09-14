package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-175: governed skills profile. The registry
// owns every product surface, but no skills surface
// exists: an employee's skills have no exposure point and
// the first surface invents skill data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that shows
// nothing until the governed growth service publishes,
// with skill truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_175(t *testing.T) {
	definition, ok := LookupPage(PageSkillsProfile)
	if !ok {
		t.Fatal("governed skills profile unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("governed skills profile incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageSkillsProfile {
		t.Fatal("skills profile route does not round-trip")
	}
	doc, err := Render(testView(PageSkillsProfile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("skills profile exposes an unresolved message key")
	}
	for _, invented := range []string{"skill:", "proficient:", "endorsed:", "verified ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("skills profile invents skill data: %q", invented)
		}
	}
}

// Golden: the registered skills profile definition and
// its fallback copy.
func TestTodo_WEB_175_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageSkillsProfile)
	if !ok {
		t.Fatal("governed skills profile unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("skills_profile.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("skills_profile.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "f9058e623c85424f1965095b2db49bb9bbc97cf1036ccd30ba6e18e4edb6e10f"
	if got != want {
		t.Fatalf("skills profile digest = %s, want %s", got, want)
	}
}

// Browser: the skills profile renders deterministically
// and round-trips its route.
func TestTodo_WEB_175_Browser(t *testing.T) {
	first, err := Render(testView(PageSkillsProfile))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageSkillsProfile))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("skills profile renders nondeterministically")
	}
	definition, _ := LookupPage(PageSkillsProfile)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageSkillsProfile {
		t.Fatal("skills profile route does not round-trip")
	}
}

// Conformance: the skills profile keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_175_Conformance(t *testing.T) {
	if !PageVisible(PageSkillsProfile, []string{"worker_self"}) {
		t.Fatal("skills profile hidden from the employee")
	}
	if PageVisible(PageSkillsProfile, nil) {
		t.Fatal("skills profile visible without roles")
	}
	definition, _ := LookupPage(PageSkillsProfile)
	if definition.PrimaryNav {
		t.Fatal("skills profile claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageSkillsProfile), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("skills profile leaks a key in %s", code)
		}
	}
}
