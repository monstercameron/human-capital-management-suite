package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-174: review-participant visibility. The
// registry owns every product surface, but no participant
// surface exists: seeing who participates in a review has
// no exposure point and the first surface invents
// participant data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that discloses nothing until the
// governed growth service publishes, with participant
// truth staying server authority — so the surface
// resolves today without a second source of business
// authority.
func TestTodo_WEB_174(t *testing.T) {
	definition, ok := LookupPage(PageReviewParticipants)
	if !ok {
		t.Fatal("review-participant visibility unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("review-participant visibility incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageReviewParticipants {
		t.Fatal("review participants route does not round-trip")
	}
	doc, err := Render(testView(PageReviewParticipants))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("review participants expose an unresolved message key")
	}
	for _, invented := range []string{"participant:", "reviewer:", "reviewee:", "listed ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("review participants invent participant data: %q", invented)
		}
	}
}

// Golden: the registered review participants definition
// and its fallback copy.
func TestTodo_WEB_174_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageReviewParticipants)
	if !ok {
		t.Fatal("review-participant visibility unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("review_participants.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("review_participants.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "3b984599111cc3cbca995aaad6ec75064627e55c337bcba77b5c01ad8c8c0364"
	if got != want {
		t.Fatalf("review participants digest = %s, want %s", got, want)
	}
}

// Browser: review participants render deterministically
// and round-trip their route.
func TestTodo_WEB_174_Browser(t *testing.T) {
	first, err := Render(testView(PageReviewParticipants))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageReviewParticipants))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("review participants render nondeterministically")
	}
	definition, _ := LookupPage(PageReviewParticipants)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageReviewParticipants {
		t.Fatal("review participants route does not round-trip")
	}
}

// Conformance: review participants keep the registry
// contract — visible to managers, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_174_Conformance(t *testing.T) {
	if !PageVisible(PageReviewParticipants, []string{"manager"}) {
		t.Fatal("review participants hidden from managers")
	}
	if PageVisible(PageReviewParticipants, nil) {
		t.Fatal("review participants visible without roles")
	}
	definition, _ := LookupPage(PageReviewParticipants)
	if definition.PrimaryNav {
		t.Fatal("review participants claim primary navigation before their service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageReviewParticipants), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("review participants leak a key in %s", code)
		}
	}
}
