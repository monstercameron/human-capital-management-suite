package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-150: time-off balance and calendar. The
// registry owns every product surface, but no time-off
// surface exists: an employee's balances and calendar
// have no exposure point and the first surface invents
// time-off data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that books nothing until the governed
// time service publishes, with time truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_150(t *testing.T) {
	definition, ok := LookupPage(PageTimeOff)
	if !ok {
		t.Fatal("time-off balance and calendar unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("time-off balance and calendar incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageTimeOff {
		t.Fatal("time-off route does not round-trip")
	}
	doc, err := Render(testView(PageTimeOff))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("time-off exposes an unresolved message key")
	}
	for _, invented := range []string{"balance:", "12 days", "booked:", "taken ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("time-off invents time-off data: %q", invented)
		}
	}
}

// Golden: the registered time-off definition and its
// fallback copy.
func TestTodo_WEB_150_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageTimeOff)
	if !ok {
		t.Fatal("time-off balance and calendar unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("time_off.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("time_off.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "c0d2e51a631035c592ad4acb3e669ed3cbe690fb184cb3db48e022fadb644354"
	if got != want {
		t.Fatalf("time-off digest = %s, want %s", got, want)
	}
}

// Browser: time-off renders deterministically and
// round-trips its route.
func TestTodo_WEB_150_Browser(t *testing.T) {
	first, err := Render(testView(PageTimeOff))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageTimeOff))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("time-off renders nondeterministically")
	}
	definition, _ := LookupPage(PageTimeOff)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageTimeOff {
		t.Fatal("time-off route does not round-trip")
	}
}

// Conformance: time-off keeps the registry contract —
// visible to the employee, hidden from the role-less
// baseline, ordered, and honest in every locale.
func TestTodo_WEB_150_Conformance(t *testing.T) {
	if !PageVisible(PageTimeOff, []string{"worker_self"}) {
		t.Fatal("time-off hidden from the employee")
	}
	if PageVisible(PageTimeOff, nil) {
		t.Fatal("time-off visible without roles")
	}
	definition, _ := LookupPage(PageTimeOff)
	if definition.PrimaryNav {
		t.Fatal("time-off claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageTimeOff), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("time-off leaks a key in %s", code)
		}
	}
}
