package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-183: effective-date organization
// navigation. The registry owns every product surface,
// but no effective-date navigation exists: seeing the org
// as of a date has no exposure point and the first
// surface invents historical org data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that navigates
// nothing until the governed organization service
// publishes, with org truth staying server authority — so
// the surface resolves today without a second source of
// business authority.
func TestTodo_WEB_183(t *testing.T) {
	definition, ok := LookupPage(PageOrgEffectiveDate)
	if !ok {
		t.Fatal("effective-date organization navigation unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("effective-date organization navigation incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageOrgEffectiveDate {
		t.Fatal("effective-date navigation route does not round-trip")
	}
	doc, err := Render(testView(PageOrgEffectiveDate))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("effective-date navigation exposes an unresolved message key")
	}
	for _, invented := range []string{"as of 2001-02-03", "had 4 divisions", "reported to Ana", "historic ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("effective-date navigation invents historical data: %q", invented)
		}
	}
}

// Golden: the registered effective-date navigation
// definition and its fallback copy.
func TestTodo_WEB_183_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageOrgEffectiveDate)
	if !ok {
		t.Fatal("effective-date organization navigation unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("org_effective_date.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("org_effective_date.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "7257bd7a8defd63a11d333ef30132e63a285a0a119d9b4bd02372881ddb176aa"
	if got != want {
		t.Fatalf("effective-date navigation digest = %s, want %s", got, want)
	}
}

// Browser: effective-date navigation renders
// deterministically and round-trips its route.
func TestTodo_WEB_183_Browser(t *testing.T) {
	first, err := Render(testView(PageOrgEffectiveDate))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageOrgEffectiveDate))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("effective-date navigation renders nondeterministically")
	}
	definition, _ := LookupPage(PageOrgEffectiveDate)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageOrgEffectiveDate {
		t.Fatal("effective-date navigation route does not round-trip")
	}
}

// Conformance: effective-date navigation keeps the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_183_Conformance(t *testing.T) {
	if !PageVisible(PageOrgEffectiveDate, []string{"worker_self"}) {
		t.Fatal("effective-date navigation hidden from the employee")
	}
	if PageVisible(PageOrgEffectiveDate, nil) {
		t.Fatal("effective-date navigation visible without roles")
	}
	definition, _ := LookupPage(PageOrgEffectiveDate)
	if definition.PrimaryNav {
		t.Fatal("effective-date navigation claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageOrgEffectiveDate), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("effective-date navigation leaks a key in %s", code)
		}
	}
}
