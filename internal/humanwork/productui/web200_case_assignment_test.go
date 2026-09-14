package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-200: case assignment and recusal. The
// registry owns every product surface, but no assignment
// surface exists: assigning cases and recording recusal
// has no exposure point and the first surface invents
// assignment data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that assigns nothing until the
// governed help service publishes, with assignment truth
// staying server authority — so the surface resolves
// today without a second source of business authority.
func TestTodo_WEB_200(t *testing.T) {
	definition, ok := LookupPage(PageCaseAssignment)
	if !ok {
		t.Fatal("case assignment and recusal unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("case assignment and recusal incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageCaseAssignment {
		t.Fatal("case assignment and recusal route does not round-trip")
	}
	doc, err := Render(testView(PageCaseAssignment))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("case assignment and recusal exposes an unresolved message key")
	}
	for _, invented := range []string{"assigned to Lena", "recused for conflict", "workload 6 open", "assignment ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("case assignment and recusal invents assignment data: %q", invented)
		}
	}
}

// Golden: the registered assignment definition and its
// fallback copy.
func TestTodo_WEB_200_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageCaseAssignment)
	if !ok {
		t.Fatal("case assignment and recusal unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("case_assignment.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("case_assignment.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "45ffc6fee1da7eb8bd63bbf31189b9dc3cc9f19489c0b8154304befded14ce45"
	if got != want {
		t.Fatalf("case assignment and recusal digest = %s, want %s", got, want)
	}
}

// Browser: case assignment and recusal renders
// deterministically and round-trips its route.
func TestTodo_WEB_200_Browser(t *testing.T) {
	first, err := Render(testView(PageCaseAssignment))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageCaseAssignment))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("case assignment and recusal renders nondeterministically")
	}
	definition, _ := LookupPage(PageCaseAssignment)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageCaseAssignment {
		t.Fatal("case assignment and recusal route does not round-trip")
	}
}

// Conformance: case assignment and recusal keeps the
// registry contract — visible to the HR specialist,
// hidden from the role-less baseline, ordered, and
// honest in every locale.
func TestTodo_WEB_200_Conformance(t *testing.T) {
	if !PageVisible(PageCaseAssignment, []string{"hr_partner"}) {
		t.Fatal("case assignment and recusal hidden from the HR specialist")
	}
	if PageVisible(PageCaseAssignment, nil) {
		t.Fatal("case assignment and recusal visible without roles")
	}
	definition, _ := LookupPage(PageCaseAssignment)
	if definition.PrimaryNav {
		t.Fatal("case assignment and recusal claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageCaseAssignment), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("case assignment and recusal leaks a key in %s", code)
		}
	}
}
