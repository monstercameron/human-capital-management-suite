package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-164: compensation calibration. The registry
// owns every product surface, but no calibration surface
// exists: calibrating ratings and awards has no exposure
// point and the first surface invents calibration state
// by convention. The compiler needs the registered
// surface — canonical identity, route, and an honest
// fallback that calibrates nothing until the governed
// compensation service publishes, with calibration truth
// staying server authority — so the surface resolves
// today without a second source of business authority.
func TestTodo_WEB_164(t *testing.T) {
	definition, ok := LookupPage(PageCompCalibration)
	if !ok {
		t.Fatal("compensation calibration unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("compensation calibration incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageCompCalibration {
		t.Fatal("compensation calibration route does not round-trip")
	}
	doc, err := Render(testView(PageCompCalibration))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("compensation calibration exposes an unresolved message key")
	}
	for _, invented := range []string{"calibration:", "rating:", "quintile 5", "locked ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("compensation calibration invents calibration data: %q", invented)
		}
	}
}

// Golden: the registered compensation calibration
// definition and its fallback copy.
func TestTodo_WEB_164_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageCompCalibration)
	if !ok {
		t.Fatal("compensation calibration unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("comp_calibration.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("comp_calibration.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "f17e495b71636a6a41ebdde92ba9fbb5854c0bb84d57a38804f15b5ffba04bfd"
	if got != want {
		t.Fatalf("compensation calibration digest = %s, want %s", got, want)
	}
}

// Browser: compensation calibration renders
// deterministically and round-trips its route.
func TestTodo_WEB_164_Browser(t *testing.T) {
	first, err := Render(testView(PageCompCalibration))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageCompCalibration))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("compensation calibration renders nondeterministically")
	}
	definition, _ := LookupPage(PageCompCalibration)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageCompCalibration {
		t.Fatal("compensation calibration route does not round-trip")
	}
}

// Conformance: compensation calibration keeps the
// registry contract — visible to managers, hidden from
// the role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_164_Conformance(t *testing.T) {
	if !PageVisible(PageCompCalibration, []string{"manager"}) {
		t.Fatal("compensation calibration hidden from managers")
	}
	if PageVisible(PageCompCalibration, nil) {
		t.Fatal("compensation calibration visible without roles")
	}
	definition, _ := LookupPage(PageCompCalibration)
	if definition.PrimaryNav {
		t.Fatal("compensation calibration claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageCompCalibration), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("compensation calibration leaks a key in %s", code)
		}
	}
}
