package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-211: final-pay and benefit status. The
// registry owns every product surface, but no final-pay
// status exists: showing final pay and benefit
// continuation has no exposure point and the first
// surface invents status data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that shows
// nothing until the governed lifecycle service
// publishes, with pay truth staying server authority —
// so the surface resolves today without a second source
// of business authority.
func TestTodo_WEB_211(t *testing.T) {
	definition, ok := LookupPage(PageFinalPay)
	if !ok {
		t.Fatal("final-pay and benefit status unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("final-pay and benefit status incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageFinalPay {
		t.Fatal("final-pay and benefit status route does not round-trip")
	}
	doc, err := Render(testView(PageFinalPay))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("final-pay and benefit status exposes an unresolved message key")
	}
	for _, invented := range []string{"final check 3,210", "COBRA starts Aug 1", "paid out Friday", "final pay ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("final-pay and benefit status invents status data: %q", invented)
		}
	}
}

// Golden: the registered final-pay definition and its
// fallback copy.
func TestTodo_WEB_211_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageFinalPay)
	if !ok {
		t.Fatal("final-pay and benefit status unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("final_pay.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("final_pay.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "fca8b615139da2306dce935b1c01e6e4d9127f88f2683b9b183ea5921d6116ea"
	if got != want {
		t.Fatalf("final-pay and benefit status digest = %s, want %s", got, want)
	}
}

// Browser: final-pay and benefit status renders
// deterministically and round-trips its route.
func TestTodo_WEB_211_Browser(t *testing.T) {
	first, err := Render(testView(PageFinalPay))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageFinalPay))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("final-pay and benefit status renders nondeterministically")
	}
	definition, _ := LookupPage(PageFinalPay)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageFinalPay {
		t.Fatal("final-pay and benefit status route does not round-trip")
	}
}

// Conformance: final-pay and benefit status keeps the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_211_Conformance(t *testing.T) {
	if !PageVisible(PageFinalPay, []string{"worker_self"}) {
		t.Fatal("final-pay and benefit status hidden from the employee")
	}
	if PageVisible(PageFinalPay, nil) {
		t.Fatal("final-pay and benefit status visible without roles")
	}
	definition, _ := LookupPage(PageFinalPay)
	if definition.PrimaryNav {
		t.Fatal("final-pay and benefit status claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageFinalPay), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("final-pay and benefit status leaks a key in %s", code)
		}
	}
}
