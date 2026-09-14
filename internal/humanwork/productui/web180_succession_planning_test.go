package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-180: succession planning. The registry owns
// every product surface, but no succession surface
// exists: planning succession has no exposure point and
// the first surface invents succession data by
// convention. The compiler needs the registered surface —
// canonical identity, route, and an honest fallback that
// plans nothing until the governed growth service
// publishes, with succession truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_180(t *testing.T) {
	definition, ok := LookupPage(PageSuccessionPlanning)
	if !ok {
		t.Fatal("succession planning unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("succession planning incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageSuccessionPlanning {
		t.Fatal("succession planning route does not round-trip")
	}
	doc, err := Render(testView(PageSuccessionPlanning))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("succession planning exposes an unresolved message key")
	}
	for _, invented := range []string{"successor:", "bench:", "candidate:", "named ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("succession planning invents succession data: %q", invented)
		}
	}
}

// Golden: the registered succession planning definition
// and its fallback copy.
func TestTodo_WEB_180_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageSuccessionPlanning)
	if !ok {
		t.Fatal("succession planning unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("succession_planning.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("succession_planning.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "47ddf78b81986f5e14555c9e0e8c0b741f70d92dd001a6e8707861fae6f02f9a"
	if got != want {
		t.Fatalf("succession planning digest = %s, want %s", got, want)
	}
}

// Browser: succession planning renders deterministically
// and round-trips its route.
func TestTodo_WEB_180_Browser(t *testing.T) {
	first, err := Render(testView(PageSuccessionPlanning))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageSuccessionPlanning))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("succession planning renders nondeterministically")
	}
	definition, _ := LookupPage(PageSuccessionPlanning)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageSuccessionPlanning {
		t.Fatal("succession planning route does not round-trip")
	}
}

// Conformance: succession planning keeps the registry
// contract — visible to managers, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_180_Conformance(t *testing.T) {
	if !PageVisible(PageSuccessionPlanning, []string{"manager"}) {
		t.Fatal("succession planning hidden from managers")
	}
	if PageVisible(PageSuccessionPlanning, nil) {
		t.Fatal("succession planning visible without roles")
	}
	definition, _ := LookupPage(PageSuccessionPlanning)
	if definition.PrimaryNav {
		t.Fatal("succession planning claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageSuccessionPlanning), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("succession planning leaks a key in %s", code)
		}
	}
}
