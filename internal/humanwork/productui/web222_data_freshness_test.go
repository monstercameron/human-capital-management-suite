package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-222: data-freshness presentation. The
// registry owns every product surface, but no freshness
// presentation exists: showing how fresh governed data
// is has no exposure point and the first surface invents
// freshness data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that presents nothing until the
// governed reporting service publishes, with freshness
// truth staying server authority — so the surface
// resolves today without a second source of business
// authority.
func TestTodo_WEB_222(t *testing.T) {
	definition, ok := LookupPage(PageDataFreshness)
	if !ok {
		t.Fatal("data-freshness presentation unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("data-freshness presentation incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageDataFreshness {
		t.Fatal("data-freshness presentation route does not round-trip")
	}
	doc, err := Render(testView(PageDataFreshness))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("data-freshness presentation exposes an unresolved message key")
	}
	for _, invented := range []string{"refreshed 5 min ago", "stale since Monday", "lag 2 hours", "freshness ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("data-freshness presentation invents freshness data: %q", invented)
		}
	}
}

// Golden: the registered freshness definition and its
// fallback copy.
func TestTodo_WEB_222_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageDataFreshness)
	if !ok {
		t.Fatal("data-freshness presentation unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("data_freshness.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("data_freshness.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "66f372deac34eb010527f1048527366024d1367a9cb93972a88fd8f54317f75e"
	if got != want {
		t.Fatalf("data-freshness presentation digest = %s, want %s", got, want)
	}
}

// Browser: data-freshness presentation renders
// deterministically and round-trips its route.
func TestTodo_WEB_222_Browser(t *testing.T) {
	first, err := Render(testView(PageDataFreshness))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageDataFreshness))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("data-freshness presentation renders nondeterministically")
	}
	definition, _ := LookupPage(PageDataFreshness)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageDataFreshness {
		t.Fatal("data-freshness presentation route does not round-trip")
	}
}

// Conformance: data-freshness presentation keeps the
// registry contract — visible to the manager, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_222_Conformance(t *testing.T) {
	if !PageVisible(PageDataFreshness, []string{"manager"}) {
		t.Fatal("data-freshness presentation hidden from the manager")
	}
	if PageVisible(PageDataFreshness, nil) {
		t.Fatal("data-freshness presentation visible without roles")
	}
	definition, _ := LookupPage(PageDataFreshness)
	if definition.PrimaryNav {
		t.Fatal("data-freshness presentation claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageDataFreshness), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("data-freshness presentation leaks a key in %s", code)
		}
	}
}
