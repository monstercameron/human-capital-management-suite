package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-147: time correction. The registry owns
// every product surface, but no time correction surface
// exists: fixing a recorded time record has no exposure
// point and the first surface invents corrections by
// convention. The compiler needs the registered surface —
// canonical identity, route, and an honest fallback that
// corrects nothing until the governed time service
// publishes, with time truth staying server authority —
// so the surface resolves today without a second source
// of business authority.
func TestTodo_WEB_147(t *testing.T) {
	definition, ok := LookupPage(PageTimeCorrection)
	if !ok {
		t.Fatal("time correction unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("time correction incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageTimeCorrection {
		t.Fatal("time correction route does not round-trip")
	}
	doc, err := Render(testView(PageTimeCorrection))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("time correction exposes an unresolved message key")
	}
	for _, invented := range []string{"corrected:", "was 0.0", "now:", "fixed ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("time correction invents time data: %q", invented)
		}
	}
}

// Golden: the registered time correction definition and
// its fallback copy.
func TestTodo_WEB_147_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageTimeCorrection)
	if !ok {
		t.Fatal("time correction unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("time_correction.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("time_correction.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "575dc0931caad770ec1e06fe13c3878625e1138e65baf9867281faeff2d2613d"
	if got != want {
		t.Fatalf("time correction digest = %s, want %s", got, want)
	}
}

// Browser: time correction renders deterministically and
// round-trips its route.
func TestTodo_WEB_147_Browser(t *testing.T) {
	first, err := Render(testView(PageTimeCorrection))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageTimeCorrection))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("time correction renders nondeterministically")
	}
	definition, _ := LookupPage(PageTimeCorrection)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageTimeCorrection {
		t.Fatal("time correction route does not round-trip")
	}
}

// Conformance: time correction keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_147_Conformance(t *testing.T) {
	if !PageVisible(PageTimeCorrection, []string{"worker_self"}) {
		t.Fatal("time correction hidden from the employee")
	}
	if PageVisible(PageTimeCorrection, nil) {
		t.Fatal("time correction visible without roles")
	}
	definition, _ := LookupPage(PageTimeCorrection)
	if definition.PrimaryNav {
		t.Fatal("time correction claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageTimeCorrection), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("time correction leaks a key in %s", code)
		}
	}
}
