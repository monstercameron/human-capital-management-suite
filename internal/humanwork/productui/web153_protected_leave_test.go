package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-153: protected-leave intake. The registry
// owns every product surface, but no protected-leave
// intake exists: starting a protected leave case has no
// exposure point and the first surface invents case state
// by convention. The compiler needs the registered
// surface — canonical identity, route, and an honest
// fallback that opens nothing until the governed leave
// service publishes, with case truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_153(t *testing.T) {
	definition, ok := LookupPage(PageProtectedLeave)
	if !ok {
		t.Fatal("protected-leave intake unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("protected-leave intake incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageProtectedLeave {
		t.Fatal("protected-leave route does not round-trip")
	}
	doc, err := Render(testView(PageProtectedLeave))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("protected-leave intake exposes an unresolved message key")
	}
	for _, invented := range []string{"case:", "open cases", "leave:", "opened ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("protected-leave intake invents case data: %q", invented)
		}
	}
}

// Golden: the registered protected-leave definition and
// its fallback copy.
func TestTodo_WEB_153_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageProtectedLeave)
	if !ok {
		t.Fatal("protected-leave intake unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("protected_leave.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("protected_leave.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "30a4e56b26512890d9f7ac5750f307422328010e6769b71db2738f915dc32f55"
	if got != want {
		t.Fatalf("protected-leave digest = %s, want %s", got, want)
	}
}

// Browser: protected-leave intake renders
// deterministically and round-trips its route.
func TestTodo_WEB_153_Browser(t *testing.T) {
	first, err := Render(testView(PageProtectedLeave))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageProtectedLeave))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("protected-leave intake renders nondeterministically")
	}
	definition, _ := LookupPage(PageProtectedLeave)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageProtectedLeave {
		t.Fatal("protected-leave route does not round-trip")
	}
}

// Conformance: protected-leave intake keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_153_Conformance(t *testing.T) {
	if !PageVisible(PageProtectedLeave, []string{"worker_self"}) {
		t.Fatal("protected-leave intake hidden from the employee")
	}
	if PageVisible(PageProtectedLeave, nil) {
		t.Fatal("protected-leave intake visible without roles")
	}
	definition, _ := LookupPage(PageProtectedLeave)
	if definition.PrimaryNav {
		t.Fatal("protected-leave intake claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageProtectedLeave), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("protected-leave intake leaks a key in %s", code)
		}
	}
}
