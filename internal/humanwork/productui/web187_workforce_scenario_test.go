package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-187: workforce scenario authoring. The
// registry owns every product surface, but no scenario
// authoring exists: drafting workforce scenarios has no
// exposure point and the first surface invents scenario
// data by convention. The compiler needs the registered
// surface — canonical identity, route, and an honest
// fallback that authors nothing until the governed
// headcount service publishes, with scenario truth
// staying server authority — so the surface resolves
// today without a second source of business authority.
func TestTodo_WEB_187(t *testing.T) {
	definition, ok := LookupPage(PageWorkforceScenario)
	if !ok {
		t.Fatal("workforce scenario authoring unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("workforce scenario authoring incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageWorkforceScenario {
		t.Fatal("workforce scenario authoring route does not round-trip")
	}
	doc, err := Render(testView(PageWorkforceScenario))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("workforce scenario authoring exposes an unresolved message key")
	}
	for _, invented := range []string{"growth scenario Q3", "hire 40 engineers", "drafted by Noa", "scenario ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("workforce scenario authoring invents scenario data: %q", invented)
		}
	}
}

// Golden: the registered workforce scenario definition
// and its fallback copy.
func TestTodo_WEB_187_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageWorkforceScenario)
	if !ok {
		t.Fatal("workforce scenario authoring unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("workforce_scenario.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("workforce_scenario.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "1bcc41551e9922a5a3966d547376cbd4717132e0178d223c4de46dc135e9de4d"
	if got != want {
		t.Fatalf("workforce scenario digest = %s, want %s", got, want)
	}
}

// Browser: workforce scenario authoring renders
// deterministically and round-trips its route.
func TestTodo_WEB_187_Browser(t *testing.T) {
	first, err := Render(testView(PageWorkforceScenario))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageWorkforceScenario))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("workforce scenario authoring renders nondeterministically")
	}
	definition, _ := LookupPage(PageWorkforceScenario)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageWorkforceScenario {
		t.Fatal("workforce scenario authoring route does not round-trip")
	}
}

// Conformance: workforce scenario authoring keeps the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_187_Conformance(t *testing.T) {
	if !PageVisible(PageWorkforceScenario, []string{"worker_self"}) {
		t.Fatal("workforce scenario authoring hidden from the employee")
	}
	if PageVisible(PageWorkforceScenario, nil) {
		t.Fatal("workforce scenario authoring visible without roles")
	}
	definition, _ := LookupPage(PageWorkforceScenario)
	if definition.PrimaryNav {
		t.Fatal("workforce scenario authoring claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageWorkforceScenario), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("workforce scenario authoring leaks a key in %s", code)
		}
	}
}
