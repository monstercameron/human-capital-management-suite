package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-160: manager compensation proposals. The
// registry owns every product surface, but no proposal
// surface exists: proposing compensation has no exposure
// point and the first surface invents proposal state by
// convention. The compiler needs the registered surface —
// canonical identity, route, and an honest fallback that
// proposes nothing until the governed compensation
// service publishes, with proposal truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_160(t *testing.T) {
	definition, ok := LookupPage(PageCompProposals)
	if !ok {
		t.Fatal("manager compensation proposals unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("manager compensation proposals incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageCompProposals {
		t.Fatal("compensation proposals route does not round-trip")
	}
	doc, err := Render(testView(PageCompProposals))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("compensation proposals expose an unresolved message key")
	}
	for _, invented := range []string{"proposal:", "proposed:", "$0.00", "draft ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("compensation proposals invent proposal data: %q", invented)
		}
	}
}

// Golden: the registered compensation proposals
// definition and its fallback copy.
func TestTodo_WEB_160_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageCompProposals)
	if !ok {
		t.Fatal("manager compensation proposals unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("comp_proposals.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("comp_proposals.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "07e105fe03c853a225197218e3e303ce7686a1b46bab8b5676100edb3fd782e6"
	if got != want {
		t.Fatalf("compensation proposals digest = %s, want %s", got, want)
	}
}

// Browser: compensation proposals render deterministically
// and round-trip their route.
func TestTodo_WEB_160_Browser(t *testing.T) {
	first, err := Render(testView(PageCompProposals))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageCompProposals))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("compensation proposals render nondeterministically")
	}
	definition, _ := LookupPage(PageCompProposals)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageCompProposals {
		t.Fatal("compensation proposals route does not round-trip")
	}
}

// Conformance: compensation proposals keep the registry
// contract — visible to managers, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_160_Conformance(t *testing.T) {
	if !PageVisible(PageCompProposals, []string{"manager"}) {
		t.Fatal("compensation proposals hidden from managers")
	}
	if PageVisible(PageCompProposals, nil) {
		t.Fatal("compensation proposals visible without roles")
	}
	definition, _ := LookupPage(PageCompProposals)
	if definition.PrimaryNav {
		t.Fatal("compensation proposals claim primary navigation before their service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageCompProposals), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("compensation proposals leak a key in %s", code)
		}
	}
}
