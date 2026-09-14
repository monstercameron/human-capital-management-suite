package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-158: accessible pay statements. The
// registry owns every product surface, but no pay
// statement surface exists: an employee's statements have
// no exposure point and the first surface invents
// statement data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that shows nothing until the governed
// pay service publishes, with statement truth staying
// server authority — so the surface resolves today
// without a second source of business authority.
func TestTodo_WEB_158(t *testing.T) {
	definition, ok := LookupPage(PagePayStatements)
	if !ok {
		t.Fatal("accessible pay statements unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("accessible pay statements incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PagePayStatements {
		t.Fatal("pay statements route does not round-trip")
	}
	doc, err := Render(testView(PagePayStatements))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("pay statements expose an unresolved message key")
	}
	for _, invented := range []string{"statement:", "period:", "$0.00", "issued ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("pay statements invent statement data: %q", invented)
		}
	}
}

// Golden: the registered pay statements definition and
// its fallback copy.
func TestTodo_WEB_158_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PagePayStatements)
	if !ok {
		t.Fatal("accessible pay statements unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("pay_statements.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("pay_statements.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "e2586b2cef936f60a5129d6e2dc8e098022c14c3db23467bb1ff5306f5c8013b"
	if got != want {
		t.Fatalf("pay statements digest = %s, want %s", got, want)
	}
}

// Browser: pay statements render deterministically and
// round-trip their route.
func TestTodo_WEB_158_Browser(t *testing.T) {
	first, err := Render(testView(PagePayStatements))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PagePayStatements))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("pay statements render nondeterministically")
	}
	definition, _ := LookupPage(PagePayStatements)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PagePayStatements {
		t.Fatal("pay statements route does not round-trip")
	}
}

// Conformance: pay statements keep the registry contract —
// visible to the employee, hidden from the role-less
// baseline, ordered, and honest in every locale.
func TestTodo_WEB_158_Conformance(t *testing.T) {
	if !PageVisible(PagePayStatements, []string{"worker_self"}) {
		t.Fatal("pay statements hidden from the employee")
	}
	if PageVisible(PagePayStatements, nil) {
		t.Fatal("pay statements visible without roles")
	}
	definition, _ := LookupPage(PagePayStatements)
	if definition.PrimaryNav {
		t.Fatal("pay statements claim primary navigation before their service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PagePayStatements), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("pay statements leak a key in %s", code)
		}
	}
}
