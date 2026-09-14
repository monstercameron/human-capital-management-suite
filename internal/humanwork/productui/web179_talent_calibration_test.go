package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-179: talent calibration. The registry owns
// every product surface, but no talent calibration
// exists: calibrating team talent has no exposure point
// and the first surface invents calibration data by
// convention. The compiler needs the registered surface —
// canonical identity, route, and an honest fallback that
// calibrates nothing until the governed growth service
// publishes, with calibration truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_179(t *testing.T) {
	definition, ok := LookupPage(PageTalentCalibration)
	if !ok {
		t.Fatal("talent calibration unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("talent calibration incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageTalentCalibration {
		t.Fatal("talent calibration route does not round-trip")
	}
	doc, err := Render(testView(PageTalentCalibration))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("talent calibration exposes an unresolved message key")
	}
	for _, invented := range []string{"calibration:", "session:", "nine-box", "placed ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("talent calibration invents calibration data: %q", invented)
		}
	}
}

// Golden: the registered talent calibration definition
// and its fallback copy.
func TestTodo_WEB_179_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageTalentCalibration)
	if !ok {
		t.Fatal("talent calibration unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("talent_calibration.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("talent_calibration.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "c626810e0ce8256e762cd525318f75f654c4066cb789a7eea874ad66d2c64414"
	if got != want {
		t.Fatalf("talent calibration digest = %s, want %s", got, want)
	}
}

// Browser: talent calibration renders deterministically
// and round-trips its route.
func TestTodo_WEB_179_Browser(t *testing.T) {
	first, err := Render(testView(PageTalentCalibration))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageTalentCalibration))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("talent calibration renders nondeterministically")
	}
	definition, _ := LookupPage(PageTalentCalibration)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageTalentCalibration {
		t.Fatal("talent calibration route does not round-trip")
	}
}

// Conformance: talent calibration keeps the registry
// contract — visible to managers, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_179_Conformance(t *testing.T) {
	if !PageVisible(PageTalentCalibration, []string{"manager"}) {
		t.Fatal("talent calibration hidden from managers")
	}
	if PageVisible(PageTalentCalibration, nil) {
		t.Fatal("talent calibration visible without roles")
	}
	definition, _ := LookupPage(PageTalentCalibration)
	if definition.PrimaryNav {
		t.Fatal("talent calibration claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageTalentCalibration), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("talent calibration leaks a key in %s", code)
		}
	}
}
