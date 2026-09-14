package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-149: time-exception workbench. The
// registry owns every product surface, but no exception
// workbench exists: resolving time exceptions has no
// exposure point and the first surface invents exception
// state by convention. The compiler needs the registered
// surface — canonical identity, route, and an honest
// fallback that resolves nothing until the governed time
// service publishes, with resolution staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_149(t *testing.T) {
	definition, ok := LookupPage(PageTimeExceptions)
	if !ok {
		t.Fatal("time-exception workbench unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("time-exception workbench incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageTimeExceptions {
		t.Fatal("time exception route does not round-trip")
	}
	doc, err := Render(testView(PageTimeExceptions))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("time exception workbench exposes an unresolved message key")
	}
	for _, invented := range []string{"exception:", "0 open", "resolved:", "cleared ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("time exception workbench invents exception data: %q", invented)
		}
	}
}

// Golden: the registered exception workbench definition
// and its fallback copy.
func TestTodo_WEB_149_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageTimeExceptions)
	if !ok {
		t.Fatal("time-exception workbench unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("time_exceptions.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("time_exceptions.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "aa748422858941ec853d89742194f557bc03b94a43c11bb7d1b77c2df6d57c60"
	if got != want {
		t.Fatalf("time exception digest = %s, want %s", got, want)
	}
}

// Browser: the exception workbench renders
// deterministically and round-trips its route.
func TestTodo_WEB_149_Browser(t *testing.T) {
	first, err := Render(testView(PageTimeExceptions))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageTimeExceptions))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("time exception workbench renders nondeterministically")
	}
	definition, _ := LookupPage(PageTimeExceptions)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageTimeExceptions {
		t.Fatal("time exception route does not round-trip")
	}
}

// Conformance: the exception workbench keeps the registry
// contract — visible to managers, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_149_Conformance(t *testing.T) {
	if !PageVisible(PageTimeExceptions, []string{"manager"}) {
		t.Fatal("time exception workbench hidden from managers")
	}
	if PageVisible(PageTimeExceptions, nil) {
		t.Fatal("time exception workbench visible without roles")
	}
	definition, _ := LookupPage(PageTimeExceptions)
	if definition.PrimaryNav {
		t.Fatal("time exception workbench claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageTimeExceptions), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("time exception workbench leaks a key in %s", code)
		}
	}
}
