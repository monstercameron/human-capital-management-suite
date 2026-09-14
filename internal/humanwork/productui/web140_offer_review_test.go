package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-140: offer review and acceptance. The
// registry owns every product surface, but no offer page
// exists: reviewing and accepting an offer has no
// exposure point and the first surface invents offer
// terms or acceptances by convention. The compiler needs
// the registered page — canonical identity, route, and
// an honest fallback that exposes no offer until the
// governed service publishes, with acceptance staying
// server authority — so the page resolves today without
// a second source of business authority.
func TestTodo_WEB_140(t *testing.T) {
	definition, ok := LookupPage(PageOffer)
	if !ok {
		t.Fatal("offer page unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("offer page incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageOffer {
		t.Fatal("offer route does not round-trip")
	}
	doc, err := Render(testView(PageOffer))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("offer page exposes an unresolved message key")
	}
	for _, invented := range []string{"Offer:", "salary:", "accepted:", "signing bonus"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("offer page invents offer data: %q", invented)
		}
	}
}

// Golden: the registered offer definition and its
// fallback copy.
func TestTodo_WEB_140_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageOffer)
	if !ok {
		t.Fatal("offer page unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("offer.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("offer.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "43d6be48ed5bd85b071291e324a14db33284ff8aa7f8be450387ef6b3ede5231"
	if got != want {
		t.Fatalf("offer digest = %s, want %s", got, want)
	}
}

// Browser: offer review and acceptance renders
// deterministically and round-trips its route.
func TestTodo_WEB_140_Browser(t *testing.T) {
	first, err := Render(testView(PageOffer))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageOffer))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("offer page renders nondeterministically")
	}
	definition, _ := LookupPage(PageOffer)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageOffer {
		t.Fatal("offer route does not round-trip")
	}
}

// Conformance: offer review and acceptance keeps the
// registry contract — visible to hiring roles, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_140_Conformance(t *testing.T) {
	if !PageVisible(PageOffer, []string{"hiring_manager"}) {
		t.Fatal("offer page hidden from hiring managers")
	}
	if PageVisible(PageOffer, nil) {
		t.Fatal("offer page visible without roles")
	}
	definition, _ := LookupPage(PageOffer)
	if definition.PrimaryNav {
		t.Fatal("offer page claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageOffer), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("offer page leaks a key in %s", code)
		}
	}
}
