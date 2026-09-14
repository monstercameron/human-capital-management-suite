package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-190: reorganization proposals. The
// registry owns every product surface, but no
// reorganization proposals exist: proposing
// reorganizations has no exposure point and the first
// surface invents proposal data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that proposes
// nothing until the governed organization service
// publishes, with proposal truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_190(t *testing.T) {
	definition, ok := LookupPage(PageReorgProposals)
	if !ok {
		t.Fatal("reorganization proposals unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("reorganization proposals incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageReorgProposals {
		t.Fatal("reorganization proposals route does not round-trip")
	}
	doc, err := Render(testView(PageReorgProposals))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("reorganization proposals expose an unresolved message key")
	}
	for _, invented := range []string{"merge sales and support", "affects 120 roles", "proposed by Wren", "reorg ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("reorganization proposals invent proposal data: %q", invented)
		}
	}
}

// Golden: the registered reorganization proposals
// definition and its fallback copy.
func TestTodo_WEB_190_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageReorgProposals)
	if !ok {
		t.Fatal("reorganization proposals unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("reorg_proposals.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("reorg_proposals.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "59f1501ceeb5ab5d4ed1b5e76b03224155b4718f223937c9d2c2e8bb9911952f"
	if got != want {
		t.Fatalf("reorganization proposals digest = %s, want %s", got, want)
	}
}

// Browser: reorganization proposals render
// deterministically and round-trip their route.
func TestTodo_WEB_190_Browser(t *testing.T) {
	first, err := Render(testView(PageReorgProposals))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageReorgProposals))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("reorganization proposals render nondeterministically")
	}
	definition, _ := LookupPage(PageReorgProposals)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageReorgProposals {
		t.Fatal("reorganization proposals route does not round-trip")
	}
}

// Conformance: reorganization proposals keep the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_190_Conformance(t *testing.T) {
	if !PageVisible(PageReorgProposals, []string{"worker_self"}) {
		t.Fatal("reorganization proposals hidden from the employee")
	}
	if PageVisible(PageReorgProposals, nil) {
		t.Fatal("reorganization proposals visible without roles")
	}
	definition, _ := LookupPage(PageReorgProposals)
	if definition.PrimaryNav {
		t.Fatal("reorganization proposals claim primary navigation before their service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageReorgProposals), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("reorganization proposals leak a key in %s", code)
		}
	}
}
