package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-208: exit review and approval. The
// registry owns every product surface, but no exit
// review exists: reviewing and approving a worker exit
// has no exposure point and the first surface invents
// review data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that reviews nothing until the
// governed lifecycle service publishes, with review
// truth staying server authority — so the surface
// resolves today without a second source of business
// authority.
func TestTodo_WEB_208(t *testing.T) {
	definition, ok := LookupPage(PageExitReview)
	if !ok {
		t.Fatal("exit review and approval unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("exit review and approval incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageExitReview {
		t.Fatal("exit review and approval route does not round-trip")
	}
	doc, err := Render(testView(PageExitReview))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("exit review and approval exposes an unresolved message key")
	}
	for _, invented := range []string{"approved by Mara", "notice waived", "exit packet ready", "review ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("exit review and approval invents review data: %q", invented)
		}
	}
}

// Golden: the registered exit review definition and
// its fallback copy.
func TestTodo_WEB_208_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageExitReview)
	if !ok {
		t.Fatal("exit review and approval unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("exit_review.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("exit_review.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "32f333f543f6af70a6e8dcc963056dd38361b794e835409b52290d9e80a9ec9a"
	if got != want {
		t.Fatalf("exit review and approval digest = %s, want %s", got, want)
	}
}

// Browser: exit review and approval renders
// deterministically and round-trips its route.
func TestTodo_WEB_208_Browser(t *testing.T) {
	first, err := Render(testView(PageExitReview))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageExitReview))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("exit review and approval renders nondeterministically")
	}
	definition, _ := LookupPage(PageExitReview)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageExitReview {
		t.Fatal("exit review and approval route does not round-trip")
	}
}

// Conformance: exit review and approval keeps the
// registry contract — visible to the manager, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_208_Conformance(t *testing.T) {
	if !PageVisible(PageExitReview, []string{"manager"}) {
		t.Fatal("exit review and approval hidden from the manager")
	}
	if PageVisible(PageExitReview, nil) {
		t.Fatal("exit review and approval visible without roles")
	}
	definition, _ := LookupPage(PageExitReview)
	if definition.PrimaryNav {
		t.Fatal("exit review and approval claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageExitReview), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("exit review and approval leaks a key in %s", code)
		}
	}
}
