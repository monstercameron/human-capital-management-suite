package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-154: restricted leave-evidence tasks. The
// registry owns every product surface, but no evidence
// surface exists: completing restricted leave evidence
// has no exposure point and the first surface invents
// evidence state by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that completes nothing until the
// governed leave service publishes, with evidence truth
// staying server authority — so the surface resolves
// today without a second source of business authority.
func TestTodo_WEB_154(t *testing.T) {
	definition, ok := LookupPage(PageLeaveEvidence)
	if !ok {
		t.Fatal("leave-evidence tasks unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("leave-evidence tasks incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageLeaveEvidence {
		t.Fatal("leave-evidence route does not round-trip")
	}
	doc, err := Render(testView(PageLeaveEvidence))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("leave-evidence tasks expose an unresolved message key")
	}
	for _, invented := range []string{"evidence:", "2 items", "uploaded:", "done ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("leave-evidence tasks invent evidence data: %q", invented)
		}
	}
}

// Golden: the registered leave-evidence definition and
// its fallback copy.
func TestTodo_WEB_154_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageLeaveEvidence)
	if !ok {
		t.Fatal("leave-evidence tasks unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("leave_evidence.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("leave_evidence.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "a9944ec4ac289fc61504b551e8f750f31c793fa0f56ec48ecab54b4d456e8c3d"
	if got != want {
		t.Fatalf("leave-evidence digest = %s, want %s", got, want)
	}
}

// Browser: leave-evidence tasks render deterministically
// and round-trip their route.
func TestTodo_WEB_154_Browser(t *testing.T) {
	first, err := Render(testView(PageLeaveEvidence))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageLeaveEvidence))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("leave-evidence tasks render nondeterministically")
	}
	definition, _ := LookupPage(PageLeaveEvidence)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageLeaveEvidence {
		t.Fatal("leave-evidence route does not round-trip")
	}
}

// Conformance: leave-evidence tasks keep the registry
// contract — visible to HR partners, hidden from the
// role-less baseline and from managers, ordered, and
// honest in every locale.
func TestTodo_WEB_154_Conformance(t *testing.T) {
	if !PageVisible(PageLeaveEvidence, []string{"hr_partner"}) {
		t.Fatal("leave-evidence tasks hidden from HR partners")
	}
	if PageVisible(PageLeaveEvidence, nil) {
		t.Fatal("leave-evidence tasks visible without roles")
	}
	if PageVisible(PageLeaveEvidence, []string{"manager"}) {
		t.Fatal("leave-evidence tasks visible to managers despite restriction")
	}
	definition, _ := LookupPage(PageLeaveEvidence)
	if definition.PrimaryNav {
		t.Fatal("leave-evidence tasks claim primary navigation before their service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageLeaveEvidence), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("leave-evidence tasks leak a key in %s", code)
		}
	}
}
