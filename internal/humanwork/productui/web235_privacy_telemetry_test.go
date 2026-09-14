package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-235: privacy-safe frontend telemetry. The
// registry owns every product surface, but no telemetry
// surface exists: instrumenting privacy-safe frontend
// telemetry has no exposure point and the first surface
// invents telemetry data by convention. The compiler
// needs the registered surface — canonical identity,
// route, and an honest fallback that instruments nothing
// until the governed telemetry service publishes, with
// telemetry truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_235(t *testing.T) {
	definition, ok := LookupPage(PagePrivacyTelemetry)
	if !ok {
		t.Fatal("privacy-safe frontend telemetry unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("privacy-safe frontend telemetry incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PagePrivacyTelemetry {
		t.Fatal("privacy-safe frontend telemetry route does not round-trip")
	}
	doc, err := Render(testView(PagePrivacyTelemetry))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("privacy-safe frontend telemetry exposes an unresolved message key")
	}
	for _, invented := range []string{"1,200 events today", "drop rate 0 pct", "traced by Vela", "telemetry ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("privacy-safe frontend telemetry invents telemetry data: %q", invented)
		}
	}
}

// Golden: the registered telemetry definition and its
// fallback copy.
func TestTodo_WEB_235_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PagePrivacyTelemetry)
	if !ok {
		t.Fatal("privacy-safe frontend telemetry unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("privacy_telemetry.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("privacy_telemetry.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "bcca905fe9f40198a16f57a30ef80073722a87f8a2f46d23212899c3ac30f3b4"
	if got != want {
		t.Fatalf("privacy-safe frontend telemetry digest = %s, want %s", got, want)
	}
}

// Browser: privacy-safe frontend telemetry renders
// deterministically and round-trips its route.
func TestTodo_WEB_235_Browser(t *testing.T) {
	first, err := Render(testView(PagePrivacyTelemetry))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PagePrivacyTelemetry))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("privacy-safe frontend telemetry renders nondeterministically")
	}
	definition, _ := LookupPage(PagePrivacyTelemetry)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PagePrivacyTelemetry {
		t.Fatal("privacy-safe frontend telemetry route does not round-trip")
	}
}

// Conformance: privacy-safe frontend telemetry keeps
// the registry contract — visible to the platform
// admin, hidden from the role-less baseline and the
// plain employee, ordered, and honest in every locale.
func TestTodo_WEB_235_Conformance(t *testing.T) {
	if !PageVisible(PagePrivacyTelemetry, []string{RoleHCMAdmin}) {
		t.Fatal("privacy-safe frontend telemetry hidden from the platform admin")
	}
	if PageVisible(PagePrivacyTelemetry, nil) {
		t.Fatal("privacy-safe frontend telemetry visible without roles")
	}
	if PageVisible(PagePrivacyTelemetry, []string{"worker_self"}) {
		t.Fatal("privacy-safe frontend telemetry visible to the plain employee")
	}
	definition, _ := LookupPage(PagePrivacyTelemetry)
	if definition.PrimaryNav {
		t.Fatal("privacy-safe frontend telemetry claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PagePrivacyTelemetry), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("privacy-safe frontend telemetry leaks a key in %s", code)
		}
	}
}
