package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-197: safe participant case status. The
// registry owns every product surface, but no safe case
// status exists: showing a participant only what they
// may see of a case has no exposure point and the first
// surface invents status data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that shows
// nothing until the governed help service publishes,
// with case truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_197(t *testing.T) {
	definition, ok := LookupPage(PageCaseStatus)
	if !ok {
		t.Fatal("safe participant case status unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("safe participant case status incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageCaseStatus {
		t.Fatal("safe participant case status route does not round-trip")
	}
	doc, err := Render(testView(PageCaseStatus))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("safe participant case status exposes an unresolved message key")
	}
	for _, invented := range []string{"your case is open", "resolved last Tuesday", "agent is Sana", "status ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("safe participant case status invents status data: %q", invented)
		}
	}
}

// Golden: the registered safe status definition and
// its fallback copy.
func TestTodo_WEB_197_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageCaseStatus)
	if !ok {
		t.Fatal("safe participant case status unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("case_status.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("case_status.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "c17a2c358e615bded9d4f6b947c79dd84c74ce6bf21f334e93426a8e4c6fd57f"
	if got != want {
		t.Fatalf("safe participant case status digest = %s, want %s", got, want)
	}
}

// Browser: safe participant case status renders
// deterministically and round-trips its route.
func TestTodo_WEB_197_Browser(t *testing.T) {
	first, err := Render(testView(PageCaseStatus))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageCaseStatus))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("safe participant case status renders nondeterministically")
	}
	definition, _ := LookupPage(PageCaseStatus)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageCaseStatus {
		t.Fatal("safe participant case status route does not round-trip")
	}
}

// Conformance: safe participant case status keeps the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_197_Conformance(t *testing.T) {
	if !PageVisible(PageCaseStatus, []string{"worker_self"}) {
		t.Fatal("safe participant case status hidden from the employee")
	}
	if PageVisible(PageCaseStatus, nil) {
		t.Fatal("safe participant case status visible without roles")
	}
	definition, _ := LookupPage(PageCaseStatus)
	if definition.PrimaryNav {
		t.Fatal("safe participant case status claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageCaseStatus), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("safe participant case status leaks a key in %s", code)
		}
	}
}
