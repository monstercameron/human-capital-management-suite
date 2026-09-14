package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-172: manager check-ins. The registry owns
// every product surface, but no check-in surface exists:
// running check-ins has no exposure point and the first
// surface invents check-in data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that runs
// nothing until the governed growth service publishes,
// with check-in truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_172(t *testing.T) {
	definition, ok := LookupPage(PageManagerCheckins)
	if !ok {
		t.Fatal("manager check-ins unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("manager check-ins incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageManagerCheckins {
		t.Fatal("manager check-ins route does not round-trip")
	}
	doc, err := Render(testView(PageManagerCheckins))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("manager check-ins expose an unresolved message key")
	}
	for _, invented := range []string{"check-in:", "notes:", "agenda:", "held ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("manager check-ins invent check-in data: %q", invented)
		}
	}
}

// Golden: the registered manager check-ins definition
// and its fallback copy.
func TestTodo_WEB_172_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageManagerCheckins)
	if !ok {
		t.Fatal("manager check-ins unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("manager_checkins.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("manager_checkins.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "8f347bb5d29518e656267bf30c576195046af10a4db3a1be25707cc94c2e8bc8"
	if got != want {
		t.Fatalf("manager check-ins digest = %s, want %s", got, want)
	}
}

// Browser: manager check-ins render deterministically and
// round-trip their route.
func TestTodo_WEB_172_Browser(t *testing.T) {
	first, err := Render(testView(PageManagerCheckins))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageManagerCheckins))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("manager check-ins render nondeterministically")
	}
	definition, _ := LookupPage(PageManagerCheckins)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageManagerCheckins {
		t.Fatal("manager check-ins route does not round-trip")
	}
}

// Conformance: manager check-ins keep the registry
// contract — visible to managers, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_172_Conformance(t *testing.T) {
	if !PageVisible(PageManagerCheckins, []string{"manager"}) {
		t.Fatal("manager check-ins hidden from managers")
	}
	if PageVisible(PageManagerCheckins, nil) {
		t.Fatal("manager check-ins visible without roles")
	}
	definition, _ := LookupPage(PageManagerCheckins)
	if definition.PrimaryNav {
		t.Fatal("manager check-ins claim primary navigation before their service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageManagerCheckins), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("manager check-ins leak a key in %s", code)
		}
	}
}
