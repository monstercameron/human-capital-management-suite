package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-178: manager talent workbench. The registry
// owns every product surface, but no talent workbench
// exists: working team talent has no exposure point and
// the first surface invents talent data by convention.
// The compiler needs the registered surface — canonical
// identity, route, and an honest fallback that shows
// nothing until the governed growth service publishes,
// with talent truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_178(t *testing.T) {
	definition, ok := LookupPage(PageTalentWorkbench)
	if !ok {
		t.Fatal("manager talent workbench unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("manager talent workbench incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageTalentWorkbench {
		t.Fatal("talent workbench route does not round-trip")
	}
	doc, err := Render(testView(PageTalentWorkbench))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("talent workbench exposes an unresolved message key")
	}
	for _, invented := range []string{"talent:", "bench:", "successor:", "ready ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("talent workbench invents talent data: %q", invented)
		}
	}
}

// Golden: the registered talent workbench definition and
// its fallback copy.
func TestTodo_WEB_178_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageTalentWorkbench)
	if !ok {
		t.Fatal("manager talent workbench unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("talent_workbench.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("talent_workbench.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "5c8d4834082e9123f534c2ab5731a1bc985b616f591437bd96afe5021b53b3c0"
	if got != want {
		t.Fatalf("talent workbench digest = %s, want %s", got, want)
	}
}

// Browser: the talent workbench renders deterministically
// and round-trips its route.
func TestTodo_WEB_178_Browser(t *testing.T) {
	first, err := Render(testView(PageTalentWorkbench))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageTalentWorkbench))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("talent workbench renders nondeterministically")
	}
	definition, _ := LookupPage(PageTalentWorkbench)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageTalentWorkbench {
		t.Fatal("talent workbench route does not round-trip")
	}
}

// Conformance: the talent workbench keeps the registry
// contract — visible to managers, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_178_Conformance(t *testing.T) {
	if !PageVisible(PageTalentWorkbench, []string{"manager"}) {
		t.Fatal("talent workbench hidden from managers")
	}
	if PageVisible(PageTalentWorkbench, nil) {
		t.Fatal("talent workbench visible without roles")
	}
	definition, _ := LookupPage(PageTalentWorkbench)
	if definition.PrimaryNav {
		t.Fatal("talent workbench claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageTalentWorkbench), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("talent workbench leaks a key in %s", code)
		}
	}
}
