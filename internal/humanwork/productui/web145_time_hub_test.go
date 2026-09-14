package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-145: employee time hub. The registry owns
// every product surface, but no time hub surface exists:
// an employee's balances, requests, and leave cases have
// no exposure point and the first surface invents time
// data by convention. The compiler needs the registered
// surface — canonical identity, route, and an honest
// fallback that records nothing until the governed time
// service publishes, with time truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_145(t *testing.T) {
	definition, ok := LookupPage(PageTimeHub)
	if !ok {
		t.Fatal("employee time hub unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("employee time hub incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageTimeHub {
		t.Fatal("time hub route does not round-trip")
	}
	doc, err := Render(testView(PageTimeHub))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("time hub exposes an unresolved message key")
	}
	for _, invented := range []string{"balance:", "0.0 hrs", "request:", "approved ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("time hub invents time data: %q", invented)
		}
	}
}

// Golden: the registered time hub definition and its
// fallback copy.
func TestTodo_WEB_145_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageTimeHub)
	if !ok {
		t.Fatal("employee time hub unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("time_hub.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("time_hub.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "cefe9fd6ff0ef799acf829494bec4fe0fe261676e1a10f0ead5aa088f98f9e29"
	if got != want {
		t.Fatalf("time hub digest = %s, want %s", got, want)
	}
}

// Browser: the time hub renders deterministically and
// round-trips its route.
func TestTodo_WEB_145_Browser(t *testing.T) {
	first, err := Render(testView(PageTimeHub))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageTimeHub))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("time hub renders nondeterministically")
	}
	definition, _ := LookupPage(PageTimeHub)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageTimeHub {
		t.Fatal("time hub route does not round-trip")
	}
}

// Conformance: the time hub keeps the registry contract —
// visible to the employee, hidden from the role-less
// baseline, ordered, and honest in every locale.
func TestTodo_WEB_145_Conformance(t *testing.T) {
	if !PageVisible(PageTimeHub, []string{"worker_self"}) {
		t.Fatal("time hub hidden from the employee")
	}
	if PageVisible(PageTimeHub, nil) {
		t.Fatal("time hub visible without roles")
	}
	definition, _ := LookupPage(PageTimeHub)
	if definition.PrimaryNav {
		t.Fatal("time hub claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageTimeHub), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("time hub leaks a key in %s", code)
		}
	}
}
