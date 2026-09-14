package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-218: distinguishing certified and customer
// reports. The registry owns every product surface, but
// no report-type distinction exists: showing which
// reports are certified against customer-built ones has
// no exposure point and the first surface invents the
// distinction by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that distinguishes nothing until the
// governed reporting service publishes, with report
// truth staying server authority — so the surface
// resolves today without a second source of business
// authority.
func TestTodo_WEB_218(t *testing.T) {
	definition, ok := LookupPage(PageReportTypes)
	if !ok {
		t.Fatal("certified and customer report distinction unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("certified and customer report distinction incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageReportTypes {
		t.Fatal("certified and customer report distinction route does not round-trip")
	}
	doc, err := Render(testView(PageReportTypes))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("certified and customer report distinction exposes an unresolved message key")
	}
	for _, invented := range []string{"8 certified reports", "customer draft v3", "certified by Zane", "reports ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("certified and customer report distinction invents distinction data: %q", invented)
		}
	}
}

// Golden: the registered report-type definition and
// its fallback copy.
func TestTodo_WEB_218_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageReportTypes)
	if !ok {
		t.Fatal("certified and customer report distinction unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("report_types.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("report_types.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "2b86043998b5df640f3b8dd67388e10a76e2b51669b96974a9cf27c94e31f493"
	if got != want {
		t.Fatalf("certified and customer report distinction digest = %s, want %s", got, want)
	}
}

// Browser: certified and customer report distinction
// renders deterministically and round-trips its route.
func TestTodo_WEB_218_Browser(t *testing.T) {
	first, err := Render(testView(PageReportTypes))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageReportTypes))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("certified and customer report distinction renders nondeterministically")
	}
	definition, _ := LookupPage(PageReportTypes)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageReportTypes {
		t.Fatal("certified and customer report distinction route does not round-trip")
	}
}

// Conformance: certified and customer report
// distinction keeps the registry contract — visible to
// the manager, hidden from the role-less baseline,
// ordered, and honest in every locale.
func TestTodo_WEB_218_Conformance(t *testing.T) {
	if !PageVisible(PageReportTypes, []string{"manager"}) {
		t.Fatal("certified and customer report distinction hidden from the manager")
	}
	if PageVisible(PageReportTypes, nil) {
		t.Fatal("certified and customer report distinction visible without roles")
	}
	definition, _ := LookupPage(PageReportTypes)
	if definition.PrimaryNav {
		t.Fatal("certified and customer report distinction claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageReportTypes), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("certified and customer report distinction leaks a key in %s", code)
		}
	}
}
