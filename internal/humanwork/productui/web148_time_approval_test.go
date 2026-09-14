package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-148: manager time approval. The registry
// owns every product surface, but no time approval
// surface exists: approving a report's time has no
// exposure point and the first surface invents approval
// state by convention. The compiler needs the registered
// surface — canonical identity, route, and an honest
// fallback that approves nothing until the governed time
// service publishes, with approval staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_148(t *testing.T) {
	definition, ok := LookupPage(PageTimeApproval)
	if !ok {
		t.Fatal("manager time approval unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("manager time approval incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageTimeApproval {
		t.Fatal("time approval route does not round-trip")
	}
	doc, err := Render(testView(PageTimeApproval))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("time approval exposes an unresolved message key")
	}
	for _, invented := range []string{"pending:", "0 requests", "approved:", "rejected ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("time approval invents approval data: %q", invented)
		}
	}
}

// Golden: the registered time approval definition and its
// fallback copy.
func TestTodo_WEB_148_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageTimeApproval)
	if !ok {
		t.Fatal("manager time approval unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("time_approval.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("time_approval.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "4e1aa5eade2868476ffdf157703a6546108b8fd30e5dd8836fe5f17c5625c9ad"
	if got != want {
		t.Fatalf("time approval digest = %s, want %s", got, want)
	}
}

// Browser: time approval renders deterministically and
// round-trips its route.
func TestTodo_WEB_148_Browser(t *testing.T) {
	first, err := Render(testView(PageTimeApproval))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageTimeApproval))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("time approval renders nondeterministically")
	}
	definition, _ := LookupPage(PageTimeApproval)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageTimeApproval {
		t.Fatal("time approval route does not round-trip")
	}
}

// Conformance: time approval keeps the registry contract —
// visible to managers, hidden from the role-less
// baseline, ordered, and honest in every locale.
func TestTodo_WEB_148_Conformance(t *testing.T) {
	if !PageVisible(PageTimeApproval, []string{"manager"}) {
		t.Fatal("time approval hidden from managers")
	}
	if PageVisible(PageTimeApproval, nil) {
		t.Fatal("time approval visible without roles")
	}
	definition, _ := LookupPage(PageTimeApproval)
	if definition.PrimaryNav {
		t.Fatal("time approval claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageTimeApproval), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("time approval leaks a key in %s", code)
		}
	}
}
