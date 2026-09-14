package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-177: career-opportunity discovery. The
// registry owns every product surface, but no opportunity
// surface exists: discovering openings has no exposure
// point and the first surface invents opportunity data by
// convention. The compiler needs the registered surface —
// canonical identity, route, and an honest fallback that
// discovers nothing until the governed growth service
// publishes, with opportunity truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_177(t *testing.T) {
	definition, ok := LookupPage(PageCareerDiscovery)
	if !ok {
		t.Fatal("career-opportunity discovery unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("career-opportunity discovery incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageCareerDiscovery {
		t.Fatal("career discovery route does not round-trip")
	}
	doc, err := Render(testView(PageCareerDiscovery))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("career discovery exposes an unresolved message key")
	}
	for _, invented := range []string{"opening:", "role:", "match:", "applied ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("career discovery invents opportunity data: %q", invented)
		}
	}
}

// Golden: the registered career discovery definition and
// its fallback copy.
func TestTodo_WEB_177_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageCareerDiscovery)
	if !ok {
		t.Fatal("career-opportunity discovery unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("career_discovery.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("career_discovery.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "55822c60b179fce4711bc527ceb4d280ba81b79d2f35cd8ac7f15ad5c45ec5ae"
	if got != want {
		t.Fatalf("career discovery digest = %s, want %s", got, want)
	}
}

// Browser: career discovery renders deterministically and
// round-trips its route.
func TestTodo_WEB_177_Browser(t *testing.T) {
	first, err := Render(testView(PageCareerDiscovery))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageCareerDiscovery))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("career discovery renders nondeterministically")
	}
	definition, _ := LookupPage(PageCareerDiscovery)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageCareerDiscovery {
		t.Fatal("career discovery route does not round-trip")
	}
}

// Conformance: career discovery keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_177_Conformance(t *testing.T) {
	if !PageVisible(PageCareerDiscovery, []string{"worker_self"}) {
		t.Fatal("career discovery hidden from the employee")
	}
	if PageVisible(PageCareerDiscovery, nil) {
		t.Fatal("career discovery visible without roles")
	}
	definition, _ := LookupPage(PageCareerDiscovery)
	if definition.PrimaryNav {
		t.Fatal("career discovery claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageCareerDiscovery), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("career discovery leaks a key in %s", code)
		}
	}
}
