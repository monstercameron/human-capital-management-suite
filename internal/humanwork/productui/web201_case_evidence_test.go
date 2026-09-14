package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-201: restricted case evidence review. The
// registry owns every product surface, but no evidence
// review exists: reviewing case evidence under
// authorization has no exposure point and the first
// surface invents evidence data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that reviews
// nothing until the governed help service publishes,
// with evidence truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_201(t *testing.T) {
	definition, ok := LookupPage(PageCaseEvidence)
	if !ok {
		t.Fatal("restricted case evidence review unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("restricted case evidence review incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageCaseEvidence {
		t.Fatal("restricted case evidence review route does not round-trip")
	}
	doc, err := Render(testView(PageCaseEvidence))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("restricted case evidence review exposes an unresolved message key")
	}
	for _, invented := range []string{"3 sealed exhibits", "verified by Nia", "chain intact ✓", "evidence ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("restricted case evidence review invents evidence data: %q", invented)
		}
	}
}

// Golden: the registered evidence review definition
// and its fallback copy.
func TestTodo_WEB_201_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageCaseEvidence)
	if !ok {
		t.Fatal("restricted case evidence review unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("case_evidence.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("case_evidence.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "035d1500b239f3aa38320b9d3000679569973c3e309725df76f6e92f52c920f9"
	if got != want {
		t.Fatalf("restricted case evidence review digest = %s, want %s", got, want)
	}
}

// Browser: restricted case evidence review renders
// deterministically and round-trips its route.
func TestTodo_WEB_201_Browser(t *testing.T) {
	first, err := Render(testView(PageCaseEvidence))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageCaseEvidence))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("restricted case evidence review renders nondeterministically")
	}
	definition, _ := LookupPage(PageCaseEvidence)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageCaseEvidence {
		t.Fatal("restricted case evidence review route does not round-trip")
	}
}

// Conformance: restricted case evidence review keeps
// the registry contract — visible to the HR specialist,
// hidden from the role-less baseline, ordered, and
// honest in every locale.
func TestTodo_WEB_201_Conformance(t *testing.T) {
	if !PageVisible(PageCaseEvidence, []string{"hr_partner"}) {
		t.Fatal("restricted case evidence review hidden from the HR specialist")
	}
	if PageVisible(PageCaseEvidence, nil) {
		t.Fatal("restricted case evidence review visible without roles")
	}
	definition, _ := LookupPage(PageCaseEvidence)
	if definition.PrimaryNav {
		t.Fatal("restricted case evidence review claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageCaseEvidence), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("restricted case evidence review leaks a key in %s", code)
		}
	}
}
