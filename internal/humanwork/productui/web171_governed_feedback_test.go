package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-171: governed feedback. The registry owns
// every product surface, but no feedback surface exists:
// exchanging governed feedback has no exposure point and
// the first surface invents feedback data by convention.
// The compiler needs the registered surface — canonical
// identity, route, and an honest fallback that exchanges
// nothing until the governed growth service publishes,
// with feedback truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_171(t *testing.T) {
	definition, ok := LookupPage(PageGovernedFeedback)
	if !ok {
		t.Fatal("governed feedback unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("governed feedback incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageGovernedFeedback {
		t.Fatal("governed feedback route does not round-trip")
	}
	doc, err := Render(testView(PageGovernedFeedback))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("governed feedback exposes an unresolved message key")
	}
	for _, invented := range []string{"feedback:", "quote:", "praise:", "shared ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("governed feedback invents feedback data: %q", invented)
		}
	}
}

// Golden: the registered governed feedback definition
// and its fallback copy.
func TestTodo_WEB_171_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageGovernedFeedback)
	if !ok {
		t.Fatal("governed feedback unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("governed_feedback.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("governed_feedback.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "a32f201bc0b99db16c9b80ee1b67d660ec6866d5e2ece38fe936c5cee62cb400"
	if got != want {
		t.Fatalf("governed feedback digest = %s, want %s", got, want)
	}
}

// Browser: governed feedback renders deterministically
// and round-trips its route.
func TestTodo_WEB_171_Browser(t *testing.T) {
	first, err := Render(testView(PageGovernedFeedback))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageGovernedFeedback))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("governed feedback renders nondeterministically")
	}
	definition, _ := LookupPage(PageGovernedFeedback)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageGovernedFeedback {
		t.Fatal("governed feedback route does not round-trip")
	}
}

// Conformance: governed feedback keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_171_Conformance(t *testing.T) {
	if !PageVisible(PageGovernedFeedback, []string{"worker_self"}) {
		t.Fatal("governed feedback hidden from the employee")
	}
	if PageVisible(PageGovernedFeedback, nil) {
		t.Fatal("governed feedback visible without roles")
	}
	definition, _ := LookupPage(PageGovernedFeedback)
	if definition.PrimaryNav {
		t.Fatal("governed feedback claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageGovernedFeedback), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("governed feedback leaks a key in %s", code)
		}
	}
}
