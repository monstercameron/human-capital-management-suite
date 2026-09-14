package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-136: the candidate pipeline. The registry
// owns every product surface, but no candidate pipeline
// exists: the candidacy lifecycle (RECRUIT-001) has no
// exposure point and the first surface invents candidate
// data by convention. The compiler needs the registered
// page — canonical identity, route, and an honest
// fallback that exposes no candidates until the governed
// service publishes — so the pipeline resolves today
// without a second source of business authority.
func TestTodo_WEB_136(t *testing.T) {
	definition, ok := LookupPage(PageCandidates)
	if !ok {
		t.Fatal("candidate pipeline unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("candidate pipeline incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageCandidates {
		t.Fatal("candidate route does not round-trip")
	}
	doc, err := Render(testView(PageCandidates))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("candidate pipeline exposes an unresolved message key")
	}
	for _, invented := range []string{"CAND-", "candidate #", "resumes:", "scores:"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("candidate pipeline invents candidate data: %q", invented)
		}
	}
}

// Golden: the registered candidate definition and its
// fallback copy.
func TestTodo_WEB_136_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageCandidates)
	if !ok {
		t.Fatal("candidate pipeline unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("candidates.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("candidates.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "38b6fcae6fc90193859fef380718d69bdb0ac30fc0ed7f512a41f5ca7c7f1c24"
	if got != want {
		t.Fatalf("candidate digest = %s, want %s", got, want)
	}
}

// Browser: the candidate pipeline renders
// deterministically and round-trips its route.
func TestTodo_WEB_136_Browser(t *testing.T) {
	first, err := Render(testView(PageCandidates))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageCandidates))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("candidate pipeline renders nondeterministically")
	}
	definition, _ := LookupPage(PageCandidates)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageCandidates {
		t.Fatal("candidate route does not round-trip")
	}
}

// Conformance: the candidate pipeline keeps the registry
// contract — visible to hiring roles, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_136_Conformance(t *testing.T) {
	if !PageVisible(PageCandidates, []string{"hiring_manager"}) {
		t.Fatal("candidate pipeline hidden from hiring managers")
	}
	if PageVisible(PageCandidates, nil) {
		t.Fatal("candidate pipeline visible without roles")
	}
	definition, _ := LookupPage(PageCandidates)
	if definition.PrimaryNav {
		t.Fatal("candidate pipeline claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageCandidates), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("candidate pipeline leaks a key in %s", code)
		}
	}
}
