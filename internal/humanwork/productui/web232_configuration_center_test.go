package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-232: the configuration center. The
// registry owns every product surface, but no
// configuration center exists: centering governed
// configuration has no exposure point and the first
// surface invents configuration data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that centers
// nothing until the governed configuration service
// publishes, with configuration truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_232(t *testing.T) {
	definition, ok := LookupPage(PageConfigurationCenter)
	if !ok {
		t.Fatal("configuration center unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("configuration center incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageConfigurationCenter {
		t.Fatal("configuration center route does not round-trip")
	}
	doc, err := Render(testView(PageConfigurationCenter))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("configuration center exposes an unresolved message key")
	}
	for _, invented := range []string{"42 settings live", "theme set globally", "edited by Ivo", "center ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("configuration center invents configuration data: %q", invented)
		}
	}
}

// Golden: the registered configuration definition and
// its fallback copy.
func TestTodo_WEB_232_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageConfigurationCenter)
	if !ok {
		t.Fatal("configuration center unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("configuration_center.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("configuration_center.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "051d3f22d69c135f6d3a6b3dd9e985db16ac41cccdb86ee50464daae54631fa8"
	if got != want {
		t.Fatalf("configuration center digest = %s, want %s", got, want)
	}
}

// Browser: configuration center renders
// deterministically and round-trips its route.
func TestTodo_WEB_232_Browser(t *testing.T) {
	first, err := Render(testView(PageConfigurationCenter))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageConfigurationCenter))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("configuration center renders nondeterministically")
	}
	definition, _ := LookupPage(PageConfigurationCenter)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageConfigurationCenter {
		t.Fatal("configuration center route does not round-trip")
	}
}

// Conformance: configuration center keeps the registry
// contract — visible to the platform admin, hidden from
// the role-less baseline and the plain employee,
// ordered, and honest in every locale.
func TestTodo_WEB_232_Conformance(t *testing.T) {
	if !PageVisible(PageConfigurationCenter, []string{RoleHCMAdmin}) {
		t.Fatal("configuration center hidden from the platform admin")
	}
	if PageVisible(PageConfigurationCenter, nil) {
		t.Fatal("configuration center visible without roles")
	}
	if PageVisible(PageConfigurationCenter, []string{"worker_self"}) {
		t.Fatal("configuration center visible to the plain employee")
	}
	definition, _ := LookupPage(PageConfigurationCenter)
	if definition.PrimaryNav {
		t.Fatal("configuration center claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageConfigurationCenter), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("configuration center leaks a key in %s", code)
		}
	}
}
