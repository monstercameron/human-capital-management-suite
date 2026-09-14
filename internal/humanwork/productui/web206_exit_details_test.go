package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-206: exit reason and effective-date
// collection. The registry owns every product surface,
// but no exit collection exists: collecting an exit
// reason and effective date has no exposure point and
// the first surface invents collection data by
// convention. The compiler needs the registered surface
// — canonical identity, route, and an honest fallback
// that collects nothing until the governed lifecycle
// service publishes, with exit truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_206(t *testing.T) {
	definition, ok := LookupPage(PageExitDetails)
	if !ok {
		t.Fatal("exit reason and effective-date collection unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("exit reason and effective-date collection incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageExitDetails {
		t.Fatal("exit reason and effective-date collection route does not round-trip")
	}
	doc, err := Render(testView(PageExitDetails))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("exit reason and effective-date collection exposes an unresolved message key")
	}
	for _, invented := range []string{"reason: new role", "effective July 1", "notice 2 weeks", "details ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("exit reason and effective-date collection invents collection data: %q", invented)
		}
	}
}

// Golden: the registered exit collection definition
// and its fallback copy.
func TestTodo_WEB_206_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageExitDetails)
	if !ok {
		t.Fatal("exit reason and effective-date collection unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("exit_details.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("exit_details.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "c378b3cfefa0fe92a195013f40ca61338178e2c0002388e94d5ea51ee211715a"
	if got != want {
		t.Fatalf("exit reason and effective-date collection digest = %s, want %s", got, want)
	}
}

// Browser: exit reason and effective-date collection
// renders deterministically and round-trips its route.
func TestTodo_WEB_206_Browser(t *testing.T) {
	first, err := Render(testView(PageExitDetails))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageExitDetails))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("exit reason and effective-date collection renders nondeterministically")
	}
	definition, _ := LookupPage(PageExitDetails)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageExitDetails {
		t.Fatal("exit reason and effective-date collection route does not round-trip")
	}
}

// Conformance: exit reason and effective-date
// collection keeps the registry contract — visible to
// the employee, hidden from the role-less baseline,
// ordered, and honest in every locale.
func TestTodo_WEB_206_Conformance(t *testing.T) {
	if !PageVisible(PageExitDetails, []string{"worker_self"}) {
		t.Fatal("exit reason and effective-date collection hidden from the employee")
	}
	if PageVisible(PageExitDetails, nil) {
		t.Fatal("exit reason and effective-date collection visible without roles")
	}
	definition, _ := LookupPage(PageExitDetails)
	if definition.PrimaryNav {
		t.Fatal("exit reason and effective-date collection claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageExitDetails), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("exit reason and effective-date collection leaks a key in %s", code)
		}
	}
}
