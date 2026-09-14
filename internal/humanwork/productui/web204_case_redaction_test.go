package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-204: case-view redaction and audit. The
// registry owns every product surface, but no redaction
// proof exists: proving that case views redact and audit
// has no exposure point and the first surface invents
// redaction data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that proves nothing until the governed
// help service publishes, with redaction truth staying
// server authority — so the surface resolves today
// without a second source of business authority.
func TestTodo_WEB_204(t *testing.T) {
	definition, ok := LookupPage(PageCaseRedaction)
	if !ok {
		t.Fatal("case-view redaction and audit unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("case-view redaction and audit incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageCaseRedaction {
		t.Fatal("case-view redaction and audit route does not round-trip")
	}
	doc, err := Render(testView(PageCaseRedaction))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("case-view redaction and audit exposes an unresolved message key")
	}
	for _, invented := range []string{"12 fields redacted", "audit trail clean", "checked by Voss", "redaction ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("case-view redaction and audit invents redaction data: %q", invented)
		}
	}
}

// Golden: the registered redaction definition and its
// fallback copy.
func TestTodo_WEB_204_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageCaseRedaction)
	if !ok {
		t.Fatal("case-view redaction and audit unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("case_redaction.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("case_redaction.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "891ada46735f85931f555c6852f370aaf37d09dcf89a77f88987ebb61ab8338a"
	if got != want {
		t.Fatalf("case-view redaction and audit digest = %s, want %s", got, want)
	}
}

// Browser: case-view redaction and audit renders
// deterministically and round-trips its route.
func TestTodo_WEB_204_Browser(t *testing.T) {
	first, err := Render(testView(PageCaseRedaction))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageCaseRedaction))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("case-view redaction and audit renders nondeterministically")
	}
	definition, _ := LookupPage(PageCaseRedaction)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageCaseRedaction {
		t.Fatal("case-view redaction and audit route does not round-trip")
	}
}

// Conformance: case-view redaction and audit keeps the
// registry contract — visible to the HR specialist,
// hidden from the role-less baseline, ordered, and
// honest in every locale.
func TestTodo_WEB_204_Conformance(t *testing.T) {
	if !PageVisible(PageCaseRedaction, []string{"hr_partner"}) {
		t.Fatal("case-view redaction and audit hidden from the HR specialist")
	}
	if PageVisible(PageCaseRedaction, nil) {
		t.Fatal("case-view redaction and audit visible without roles")
	}
	definition, _ := LookupPage(PageCaseRedaction)
	if definition.PrimaryNav {
		t.Fatal("case-view redaction and audit claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageCaseRedaction), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("case-view redaction and audit leaks a key in %s", code)
		}
	}
}
