package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-227: analysis-to-proposal handoff. The
// registry owns every product surface, but no handoff
// surface exists: handing analysis off into a proposal
// has no exposure point and the first surface invents
// handoff data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that hands off nothing until the
// governed reporting service publishes, with handoff
// truth staying server authority — so the surface
// resolves today without a second source of business
// authority.
func TestTodo_WEB_227(t *testing.T) {
	definition, ok := LookupPage(PageAnalysisHandoff)
	if !ok {
		t.Fatal("analysis-to-proposal handoff unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("analysis-to-proposal handoff incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageAnalysisHandoff {
		t.Fatal("analysis-to-proposal handoff route does not round-trip")
	}
	doc, err := Render(testView(PageAnalysisHandoff))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("analysis-to-proposal handoff exposes an unresolved message key")
	}
	for _, invented := range []string{"handed to comp cycle", "proposal draft 5", "sent by Wren", "handoff ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("analysis-to-proposal handoff invents handoff data: %q", invented)
		}
	}
}

// Golden: the registered handoff definition and its
// fallback copy.
func TestTodo_WEB_227_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageAnalysisHandoff)
	if !ok {
		t.Fatal("analysis-to-proposal handoff unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("analysis_handoff.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("analysis_handoff.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "54019f79e9c520a57a0c85360a7df232641127a11451c153e08241500ea6b9bc"
	if got != want {
		t.Fatalf("analysis-to-proposal handoff digest = %s, want %s", got, want)
	}
}

// Browser: analysis-to-proposal handoff renders
// deterministically and round-trips its route.
func TestTodo_WEB_227_Browser(t *testing.T) {
	first, err := Render(testView(PageAnalysisHandoff))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageAnalysisHandoff))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("analysis-to-proposal handoff renders nondeterministically")
	}
	definition, _ := LookupPage(PageAnalysisHandoff)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageAnalysisHandoff {
		t.Fatal("analysis-to-proposal handoff route does not round-trip")
	}
}

// Conformance: analysis-to-proposal handoff keeps the
// registry contract — visible to the manager, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_227_Conformance(t *testing.T) {
	if !PageVisible(PageAnalysisHandoff, []string{"manager"}) {
		t.Fatal("analysis-to-proposal handoff hidden from the manager")
	}
	if PageVisible(PageAnalysisHandoff, nil) {
		t.Fatal("analysis-to-proposal handoff visible without roles")
	}
	definition, _ := LookupPage(PageAnalysisHandoff)
	if definition.PrimaryNav {
		t.Fatal("analysis-to-proposal handoff claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageAnalysisHandoff), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("analysis-to-proposal handoff leaks a key in %s", code)
		}
	}
}
