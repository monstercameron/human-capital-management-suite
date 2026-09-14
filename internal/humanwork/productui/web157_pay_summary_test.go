package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-157: employee pay summary. The registry
// owns every product surface, but no pay summary exists:
// an employee's pay has no exposure point and the first
// surface invents pay figures by convention. The compiler
// needs the registered surface — canonical identity,
// route, and an honest fallback that shows nothing until
// the governed pay service publishes, with pay truth
// staying server authority — so the surface resolves
// today without a second source of business authority.
func TestTodo_WEB_157(t *testing.T) {
	definition, ok := LookupPage(PagePaySummary)
	if !ok {
		t.Fatal("employee pay summary unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("employee pay summary incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PagePaySummary {
		t.Fatal("pay summary route does not round-trip")
	}
	doc, err := Render(testView(PagePaySummary))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("pay summary exposes an unresolved message key")
	}
	for _, invented := range []string{"gross:", "$0.00", "net pay:", "paid ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("pay summary invents pay data: %q", invented)
		}
	}
}

// Golden: the registered pay summary definition and its
// fallback copy.
func TestTodo_WEB_157_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PagePaySummary)
	if !ok {
		t.Fatal("employee pay summary unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("pay_summary.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("pay_summary.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "ea2208fbe7bf579b33e877db1cdbd6a10a5c5e64cc8a44cd2e18fa6ceec8805f"
	if got != want {
		t.Fatalf("pay summary digest = %s, want %s", got, want)
	}
}

// Browser: the pay summary renders deterministically and
// round-trips its route.
func TestTodo_WEB_157_Browser(t *testing.T) {
	first, err := Render(testView(PagePaySummary))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PagePaySummary))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("pay summary renders nondeterministically")
	}
	definition, _ := LookupPage(PagePaySummary)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PagePaySummary {
		t.Fatal("pay summary route does not round-trip")
	}
}

// Conformance: the pay summary keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_157_Conformance(t *testing.T) {
	if !PageVisible(PagePaySummary, []string{"worker_self"}) {
		t.Fatal("pay summary hidden from the employee")
	}
	if PageVisible(PagePaySummary, nil) {
		t.Fatal("pay summary visible without roles")
	}
	definition, _ := LookupPage(PagePaySummary)
	if definition.PrimaryNav {
		t.Fatal("pay summary claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PagePaySummary), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("pay summary leaks a key in %s", code)
		}
	}
}
