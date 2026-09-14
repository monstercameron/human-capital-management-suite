package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-225: authorized report sharing. The
// registry owns every product surface, but no sharing
// surface exists: sharing governed reports under
// authorization has no exposure point and the first
// surface invents sharing data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that shares
// nothing until the governed reporting service
// publishes, with sharing truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_225(t *testing.T) {
	definition, ok := LookupPage(PageReportSharing)
	if !ok {
		t.Fatal("authorized report sharing unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("authorized report sharing incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageReportSharing {
		t.Fatal("authorized report sharing route does not round-trip")
	}
	doc, err := Render(testView(PageReportSharing))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("authorized report sharing exposes an unresolved message key")
	}
	for _, invented := range []string{"shared with Priya", "link expires Friday", "viewed 11 times", "sharing ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("authorized report sharing invents sharing data: %q", invented)
		}
	}
}

// Golden: the registered sharing definition and its
// fallback copy.
func TestTodo_WEB_225_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageReportSharing)
	if !ok {
		t.Fatal("authorized report sharing unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("report_sharing.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("report_sharing.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "1f003ab57aa110cf46c74edf964eabf94960af0e9b15e0b281f2f365ffa7e63d"
	if got != want {
		t.Fatalf("authorized report sharing digest = %s, want %s", got, want)
	}
}

// Browser: authorized report sharing renders
// deterministically and round-trips its route.
func TestTodo_WEB_225_Browser(t *testing.T) {
	first, err := Render(testView(PageReportSharing))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageReportSharing))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("authorized report sharing renders nondeterministically")
	}
	definition, _ := LookupPage(PageReportSharing)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageReportSharing {
		t.Fatal("authorized report sharing route does not round-trip")
	}
}

// Conformance: authorized report sharing keeps the
// registry contract — visible to the manager, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_225_Conformance(t *testing.T) {
	if !PageVisible(PageReportSharing, []string{"manager"}) {
		t.Fatal("authorized report sharing hidden from the manager")
	}
	if PageVisible(PageReportSharing, nil) {
		t.Fatal("authorized report sharing visible without roles")
	}
	definition, _ := LookupPage(PageReportSharing)
	if definition.PrimaryNav {
		t.Fatal("authorized report sharing claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageReportSharing), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("authorized report sharing leaks a key in %s", code)
		}
	}
}
