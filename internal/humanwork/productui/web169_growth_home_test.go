package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-169: employee Growth home. The registry
// owns every product surface, but no Growth home exists:
// an employee's growth has no exposure point and the
// first surface invents growth data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that shows
// nothing until the governed growth service publishes,
// with growth truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_169(t *testing.T) {
	definition, ok := LookupPage(PageGrowthHome)
	if !ok {
		t.Fatal("employee Growth home unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("employee Growth home incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageGrowthHome {
		t.Fatal("Growth home route does not round-trip")
	}
	doc, err := Render(testView(PageGrowthHome))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("Growth home exposes an unresolved message key")
	}
	for _, invented := range []string{"growth:", "level:", "mentor:", "advanced ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("Growth home invents growth data: %q", invented)
		}
	}
}

// Golden: the registered Growth home definition and its
// fallback copy.
func TestTodo_WEB_169_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageGrowthHome)
	if !ok {
		t.Fatal("employee Growth home unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("growth_home.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("growth_home.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "1804a345df891281f6a0b995af3bf3a70c6596b0cd3e875e144aa1f070ba4b7e"
	if got != want {
		t.Fatalf("Growth home digest = %s, want %s", got, want)
	}
}

// Browser: the Growth home renders deterministically and
// round-trips its route.
func TestTodo_WEB_169_Browser(t *testing.T) {
	first, err := Render(testView(PageGrowthHome))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageGrowthHome))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("Growth home renders nondeterministically")
	}
	definition, _ := LookupPage(PageGrowthHome)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageGrowthHome {
		t.Fatal("Growth home route does not round-trip")
	}
}

// Conformance: the Growth home keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_169_Conformance(t *testing.T) {
	if !PageVisible(PageGrowthHome, []string{"worker_self"}) {
		t.Fatal("Growth home hidden from the employee")
	}
	if PageVisible(PageGrowthHome, nil) {
		t.Fatal("Growth home visible without roles")
	}
	definition, _ := LookupPage(PageGrowthHome)
	if definition.PrimaryNav {
		t.Fatal("Growth home claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageGrowthHome), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("Growth home leaks a key in %s", code)
		}
	}
}
