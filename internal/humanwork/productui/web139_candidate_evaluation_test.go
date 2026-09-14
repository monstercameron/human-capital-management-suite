package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-139: structured candidate evaluation. The
// registry owns every product surface, but no evaluation
// page exists: structured candidate evaluation has no
// exposure point and the first surface invents criteria,
// ratings, or recommendations by convention. The
// compiler needs the registered page — canonical
// identity, route, and an honest fallback that exposes
// no evaluation until the governed service publishes —
// so evaluation resolves today without a second source
// of business authority.
func TestTodo_WEB_139(t *testing.T) {
	definition, ok := LookupPage(PageEvaluation)
	if !ok {
		t.Fatal("candidate evaluation unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("candidate evaluation incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageEvaluation {
		t.Fatal("evaluation route does not round-trip")
	}
	doc, err := Render(testView(PageEvaluation))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("candidate evaluation exposes an unresolved message key")
	}
	for _, invented := range []string{"Score:", "rating:", "recommend:", "hire/no-hire"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("candidate evaluation invents evaluation data: %q", invented)
		}
	}
}

// Golden: the registered evaluation definition and its
// fallback copy.
func TestTodo_WEB_139_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageEvaluation)
	if !ok {
		t.Fatal("candidate evaluation unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("evaluation.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("evaluation.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "03ca858a177d211aa99151f3ddac2e13fd2e691121a982a86795ace20d8ee0d6"
	if got != want {
		t.Fatalf("evaluation digest = %s, want %s", got, want)
	}
}

// Browser: structured candidate evaluation renders
// deterministically and round-trips its route.
func TestTodo_WEB_139_Browser(t *testing.T) {
	first, err := Render(testView(PageEvaluation))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageEvaluation))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("candidate evaluation renders nondeterministically")
	}
	definition, _ := LookupPage(PageEvaluation)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageEvaluation {
		t.Fatal("evaluation route does not round-trip")
	}
}

// Conformance: structured candidate evaluation keeps the
// registry contract — visible to hiring roles, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_139_Conformance(t *testing.T) {
	if !PageVisible(PageEvaluation, []string{"hiring_manager"}) {
		t.Fatal("candidate evaluation hidden from hiring managers")
	}
	if PageVisible(PageEvaluation, nil) {
		t.Fatal("candidate evaluation visible without roles")
	}
	definition, _ := LookupPage(PageEvaluation)
	if definition.PrimaryNav {
		t.Fatal("candidate evaluation claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageEvaluation), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("candidate evaluation leaks a key in %s", code)
		}
	}
}
