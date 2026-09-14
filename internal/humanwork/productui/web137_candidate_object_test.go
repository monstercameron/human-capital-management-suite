package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-137: the candidate object page. The
// registry owns every product surface, but no candidate
// object page exists: a single candidacy has no exposure
// point and the first surface invents candidate facts by
// convention. The compiler needs the registered page —
// canonical identity, route, and an honest fallback that
// exposes no candidate facts until the governed service
// publishes — so the object page resolves today without
// a second source of business authority.
func TestTodo_WEB_137(t *testing.T) {
	definition, ok := LookupPage(PageCandidate)
	if !ok {
		t.Fatal("candidate object page unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("candidate object page incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageCandidate {
		t.Fatal("candidate object route does not round-trip")
	}
	doc, err := Render(testView(PageCandidate))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("candidate object page exposes an unresolved message key")
	}
	for _, invented := range []string{"CAND-", "candidate #", "resumes:", "scores:"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("candidate object page invents candidate facts: %q", invented)
		}
	}
}

// Golden: the registered candidate object definition and
// its fallback copy.
func TestTodo_WEB_137_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageCandidate)
	if !ok {
		t.Fatal("candidate object page unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("candidate.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("candidate.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "44faceb41929e41203259604eb3893d18af63a59fada745fe543cb43a578f78b"
	if got != want {
		t.Fatalf("candidate object digest = %s, want %s", got, want)
	}
}

// Browser: the candidate object page renders
// deterministically and round-trips its route.
func TestTodo_WEB_137_Browser(t *testing.T) {
	first, err := Render(testView(PageCandidate))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageCandidate))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("candidate object page renders nondeterministically")
	}
	definition, _ := LookupPage(PageCandidate)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageCandidate {
		t.Fatal("candidate object route does not round-trip")
	}
}

// Conformance: the candidate object page keeps the
// registry contract — visible to hiring roles, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_137_Conformance(t *testing.T) {
	if !PageVisible(PageCandidate, []string{"hiring_manager"}) {
		t.Fatal("candidate object page hidden from hiring managers")
	}
	if PageVisible(PageCandidate, nil) {
		t.Fatal("candidate object page visible without roles")
	}
	definition, _ := LookupPage(PageCandidate)
	if definition.PrimaryNav {
		t.Fatal("candidate object page claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageCandidate), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("candidate object page leaks a key in %s", code)
		}
	}
}
