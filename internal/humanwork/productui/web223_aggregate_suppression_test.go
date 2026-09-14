package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-223: aggregate suppression states. The
// registry owns every product surface, but no
// suppression surface exists: showing where small
// aggregates are suppressed has no exposure point and
// the first surface invents suppression data by
// convention. The compiler needs the registered surface
// — canonical identity, route, and an honest fallback
// that suppresses nothing until the governed reporting
// service publishes, with suppression truth staying
// server authority — so the surface resolves today
// without a second source of business authority.
func TestTodo_WEB_223(t *testing.T) {
	definition, ok := LookupPage(PageAggregateSuppression)
	if !ok {
		t.Fatal("aggregate suppression states unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("aggregate suppression states incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageAggregateSuppression {
		t.Fatal("aggregate suppression states route does not round-trip")
	}
	doc, err := Render(testView(PageAggregateSuppression))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("aggregate suppression states expose an unresolved message key")
	}
	for _, invented := range []string{"5 cells suppressed", "threshold is 10", "hidden for privacy", "suppressed ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("aggregate suppression states invent suppression data: %q", invented)
		}
	}
}

// Golden: the registered suppression definition and
// its fallback copy.
func TestTodo_WEB_223_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageAggregateSuppression)
	if !ok {
		t.Fatal("aggregate suppression states unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("aggregate_suppression.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("aggregate_suppression.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "1c1d43e91cf85af0690f10afff36ce84b408323df4c33555267c8d58bc14a9f8"
	if got != want {
		t.Fatalf("aggregate suppression states digest = %s, want %s", got, want)
	}
}

// Browser: aggregate suppression states render
// deterministically and round-trip their route.
func TestTodo_WEB_223_Browser(t *testing.T) {
	first, err := Render(testView(PageAggregateSuppression))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageAggregateSuppression))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("aggregate suppression states render nondeterministically")
	}
	definition, _ := LookupPage(PageAggregateSuppression)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageAggregateSuppression {
		t.Fatal("aggregate suppression states route does not round-trip")
	}
}

// Conformance: aggregate suppression states keep the
// registry contract — visible to the manager, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_223_Conformance(t *testing.T) {
	if !PageVisible(PageAggregateSuppression, []string{"manager"}) {
		t.Fatal("aggregate suppression states hidden from the manager")
	}
	if PageVisible(PageAggregateSuppression, nil) {
		t.Fatal("aggregate suppression states visible without roles")
	}
	definition, _ := LookupPage(PageAggregateSuppression)
	if definition.PrimaryNav {
		t.Fatal("aggregate suppression states claim primary navigation before their service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageAggregateSuppression), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("aggregate suppression states leak a key in %s", code)
		}
	}
}
