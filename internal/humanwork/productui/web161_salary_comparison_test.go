package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-161: salary-range and budget comparison.
// The registry owns every product surface, but no salary
// comparison exists: comparing ranges against budgets has
// no exposure point and the first surface invents band
// data by convention. The compiler needs the registered
// surface — canonical identity, route, and an honest
// fallback that compares nothing until the governed
// compensation service publishes, with band truth staying
// server authority — so the surface resolves today
// without a second source of business authority.
func TestTodo_WEB_161(t *testing.T) {
	definition, ok := LookupPage(PageSalaryComparison)
	if !ok {
		t.Fatal("salary-range and budget comparison unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("salary-range and budget comparison incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageSalaryComparison {
		t.Fatal("salary comparison route does not round-trip")
	}
	doc, err := Render(testView(PageSalaryComparison))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("salary comparison exposes an unresolved message key")
	}
	content := webMainVisibleText(t, doc)
	for _, invented := range []string{"range:", "band:", "$0.00", "within ✓"} {
		if strings.Contains(content, invented) {
			t.Fatalf("salary comparison invents band data: %q", invented)
		}
	}
}

// Golden: the registered salary comparison definition
// and its fallback copy.
func TestTodo_WEB_161_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageSalaryComparison)
	if !ok {
		t.Fatal("salary-range and budget comparison unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("salary_comparison.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("salary_comparison.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "d506f9c266a3d8f32817b4897354124e432d4a72776726f39a6a2691fd5e3413"
	if got != want {
		t.Fatalf("salary comparison digest = %s, want %s", got, want)
	}
}

// Browser: salary comparison renders deterministically
// and round-trips its route.
func TestTodo_WEB_161_Browser(t *testing.T) {
	first, err := Render(testView(PageSalaryComparison))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageSalaryComparison))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("salary comparison renders nondeterministically")
	}
	definition, _ := LookupPage(PageSalaryComparison)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageSalaryComparison {
		t.Fatal("salary comparison route does not round-trip")
	}
}

// Conformance: salary comparison keeps the registry
// contract — visible to managers, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_161_Conformance(t *testing.T) {
	if !PageVisible(PageSalaryComparison, []string{"manager"}) {
		t.Fatal("salary comparison hidden from managers")
	}
	if PageVisible(PageSalaryComparison, nil) {
		t.Fatal("salary comparison visible without roles")
	}
	definition, _ := LookupPage(PageSalaryComparison)
	if definition.PrimaryNav {
		t.Fatal("salary comparison claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageSalaryComparison), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("salary comparison leaks a key in %s", code)
		}
	}
}
