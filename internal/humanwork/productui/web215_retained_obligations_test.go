package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-215: retained-obligation presentation.
// The registry owns every product surface, but no
// retained-obligation presentation exists: showing
// obligations that survive an exit has no exposure point
// and the first surface invents obligation data by
// convention. The compiler needs the registered surface
// — canonical identity, route, and an honest fallback
// that presents nothing until the governed lifecycle
// service publishes, with obligation truth staying
// server authority — so the surface resolves today
// without a second source of business authority.
func TestTodo_WEB_215(t *testing.T) {
	definition, ok := LookupPage(PageRetainedObligations)
	if !ok {
		t.Fatal("retained-obligation presentation unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("retained-obligation presentation incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageRetainedObligations {
		t.Fatal("retained-obligation presentation route does not round-trip")
	}
	doc, err := Render(testView(PageRetainedObligations))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("retained-obligation presentation exposes an unresolved message key")
	}
	for _, invented := range []string{"non-compete 12 months", "IP assigned fully", "acknowledged by Quin", "obligation ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("retained-obligation presentation invents obligation data: %q", invented)
		}
	}
}

// Golden: the registered retained-obligation definition
// and its fallback copy.
func TestTodo_WEB_215_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageRetainedObligations)
	if !ok {
		t.Fatal("retained-obligation presentation unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("retained_obligations.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("retained_obligations.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "3d7cfa8bf441b1722163c9cff8e2b726fbbe69a42fd2d62a93ac4d267744ae4f"
	if got != want {
		t.Fatalf("retained-obligation presentation digest = %s, want %s", got, want)
	}
}

// Browser: retained-obligation presentation renders
// deterministically and round-trips its route.
func TestTodo_WEB_215_Browser(t *testing.T) {
	first, err := Render(testView(PageRetainedObligations))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageRetainedObligations))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("retained-obligation presentation renders nondeterministically")
	}
	definition, _ := LookupPage(PageRetainedObligations)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageRetainedObligations {
		t.Fatal("retained-obligation presentation route does not round-trip")
	}
}

// Conformance: retained-obligation presentation keeps
// the registry contract — visible to the employee,
// hidden from the role-less baseline, ordered, and
// honest in every locale.
func TestTodo_WEB_215_Conformance(t *testing.T) {
	if !PageVisible(PageRetainedObligations, []string{"worker_self"}) {
		t.Fatal("retained-obligation presentation hidden from the employee")
	}
	if PageVisible(PageRetainedObligations, nil) {
		t.Fatal("retained-obligation presentation visible without roles")
	}
	definition, _ := LookupPage(PageRetainedObligations)
	if definition.PrimaryNav {
		t.Fatal("retained-obligation presentation claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageRetainedObligations), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("retained-obligation presentation leaks a key in %s", code)
		}
	}
}
