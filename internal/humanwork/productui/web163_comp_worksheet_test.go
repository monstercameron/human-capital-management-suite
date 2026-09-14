package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-163: compensation worksheet. The registry
// owns every product surface, but no worksheet exists:
// working the compensation cycle has no exposure point
// and the first surface invents worksheet rows by
// convention. The compiler needs the registered surface —
// canonical identity, route, and an honest fallback that
// shows nothing until the governed compensation service
// publishes, with worksheet truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_163(t *testing.T) {
	definition, ok := LookupPage(PageCompWorksheet)
	if !ok {
		t.Fatal("compensation worksheet unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("compensation worksheet incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageCompWorksheet {
		t.Fatal("compensation worksheet route does not round-trip")
	}
	doc, err := Render(testView(PageCompWorksheet))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("compensation worksheet exposes an unresolved message key")
	}
	for _, invented := range []string{"worksheet:", "merit 3%", "$0.00", "saved ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("compensation worksheet invents worksheet data: %q", invented)
		}
	}
}

// Golden: the registered compensation worksheet
// definition and its fallback copy.
func TestTodo_WEB_163_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageCompWorksheet)
	if !ok {
		t.Fatal("compensation worksheet unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("comp_worksheet.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("comp_worksheet.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "c13adfd1d2302aabbe7f0d8d25fb55f1360dbaab434357186af0b6310548c3d9"
	if got != want {
		t.Fatalf("compensation worksheet digest = %s, want %s", got, want)
	}
}

// Browser: the compensation worksheet renders
// deterministically and round-trips its route.
func TestTodo_WEB_163_Browser(t *testing.T) {
	first, err := Render(testView(PageCompWorksheet))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageCompWorksheet))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("compensation worksheet renders nondeterministically")
	}
	definition, _ := LookupPage(PageCompWorksheet)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageCompWorksheet {
		t.Fatal("compensation worksheet route does not round-trip")
	}
}

// Conformance: the compensation worksheet keeps the
// registry contract — visible to managers, hidden from
// the role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_163_Conformance(t *testing.T) {
	if !PageVisible(PageCompWorksheet, []string{"manager"}) {
		t.Fatal("compensation worksheet hidden from managers")
	}
	if PageVisible(PageCompWorksheet, nil) {
		t.Fatal("compensation worksheet visible without roles")
	}
	definition, _ := LookupPage(PageCompWorksheet)
	if definition.PrimaryNav {
		t.Fatal("compensation worksheet claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageCompWorksheet), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("compensation worksheet leaks a key in %s", code)
		}
	}
}
