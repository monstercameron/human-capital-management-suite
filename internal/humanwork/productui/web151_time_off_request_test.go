package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-151: time-off request journey. The
// registry owns every product surface, but no request
// journey exists: asking for time off has no exposure
// point and the first surface invents request state by
// convention. The compiler needs the registered surface —
// canonical identity, route, and an honest fallback that
// requests nothing until the governed time service
// publishes, with request truth staying server authority
// — so the surface resolves today without a second
// source of business authority.
func TestTodo_WEB_151(t *testing.T) {
	definition, ok := LookupPage(PageTimeOffRequest)
	if !ok {
		t.Fatal("time-off request journey unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("time-off request journey incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageTimeOffRequest {
		t.Fatal("time-off request route does not round-trip")
	}
	doc, err := Render(testView(PageTimeOffRequest))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("time-off request exposes an unresolved message key")
	}
	for _, invented := range []string{"request:", "3 days", "2001-02-03", "pending ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("time-off request invents request data: %q", invented)
		}
	}
}

// Golden: the registered request journey definition and
// its fallback copy.
func TestTodo_WEB_151_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageTimeOffRequest)
	if !ok {
		t.Fatal("time-off request journey unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("time_off_request.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("time_off_request.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "cbbb6f2dbaa02f8b425b6f641e9bce5180ad6dbf4a78f3cbd9e703adf9d96cd0"
	if got != want {
		t.Fatalf("time-off request digest = %s, want %s", got, want)
	}
}

// Browser: the request journey renders deterministically
// and round-trips its route.
func TestTodo_WEB_151_Browser(t *testing.T) {
	first, err := Render(testView(PageTimeOffRequest))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageTimeOffRequest))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("time-off request renders nondeterministically")
	}
	definition, _ := LookupPage(PageTimeOffRequest)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageTimeOffRequest {
		t.Fatal("time-off request route does not round-trip")
	}
}

// Conformance: the request journey keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_151_Conformance(t *testing.T) {
	if !PageVisible(PageTimeOffRequest, []string{"worker_self"}) {
		t.Fatal("time-off request hidden from the employee")
	}
	if PageVisible(PageTimeOffRequest, nil) {
		t.Fatal("time-off request visible without roles")
	}
	definition, _ := LookupPage(PageTimeOffRequest)
	if definition.PrimaryNav {
		t.Fatal("time-off request claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageTimeOffRequest), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("time-off request leaks a key in %s", code)
		}
	}
}
