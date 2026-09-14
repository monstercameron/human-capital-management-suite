package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-146: accessible time entry. The registry
// owns every product surface, but no time entry surface
// exists: recording hours worked has no exposure point
// and the first surface invents time records by
// convention. The compiler needs the registered surface —
// canonical identity, route, and an honest fallback that
// records nothing until the governed time service
// publishes, with time truth staying server authority —
// so the surface resolves today without a second source
// of business authority.
func TestTodo_WEB_146(t *testing.T) {
	definition, ok := LookupPage(PageTimeEntry)
	if !ok {
		t.Fatal("accessible time entry unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("accessible time entry incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageTimeEntry {
		t.Fatal("time entry route does not round-trip")
	}
	doc, err := Render(testView(PageTimeEntry))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("time entry exposes an unresolved message key")
	}
	for _, invented := range []string{"hours:", "0.0 hrs", "saved:", "submitted ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("time entry invents time data: %q", invented)
		}
	}
}

// Golden: the registered time entry definition and its
// fallback copy.
func TestTodo_WEB_146_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageTimeEntry)
	if !ok {
		t.Fatal("accessible time entry unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("time_entry.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("time_entry.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "ba1f0220f353cf2acff5e6a270f4969d3795211759e729210d61fb2bf0302083"
	if got != want {
		t.Fatalf("time entry digest = %s, want %s", got, want)
	}
}

// Browser: time entry renders deterministically and
// round-trips its route.
func TestTodo_WEB_146_Browser(t *testing.T) {
	first, err := Render(testView(PageTimeEntry))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageTimeEntry))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("time entry renders nondeterministically")
	}
	definition, _ := LookupPage(PageTimeEntry)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageTimeEntry {
		t.Fatal("time entry route does not round-trip")
	}
}

// Conformance: time entry keeps the registry contract —
// visible to the employee, hidden from the role-less
// baseline, ordered, and honest in every locale.
func TestTodo_WEB_146_Conformance(t *testing.T) {
	if !PageVisible(PageTimeEntry, []string{"worker_self"}) {
		t.Fatal("time entry hidden from the employee")
	}
	if PageVisible(PageTimeEntry, nil) {
		t.Fatal("time entry visible without roles")
	}
	definition, _ := LookupPage(PageTimeEntry)
	if definition.PrimaryNav {
		t.Fatal("time entry claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageTimeEntry), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("time entry leaks a key in %s", code)
		}
	}
}
