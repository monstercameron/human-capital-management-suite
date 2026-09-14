package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-216: exit completion and correction. The
// registry owns every product surface, but no exit
// completion exists: completing and correcting a worker
// exit has no exposure point and the first surface
// invents completion data by convention. The compiler
// needs the registered surface — canonical identity,
// route, and an honest fallback that completes nothing
// until the governed lifecycle service publishes, with
// completion truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_216(t *testing.T) {
	definition, ok := LookupPage(PageExitCompletion)
	if !ok {
		t.Fatal("exit completion and correction unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("exit completion and correction incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageExitCompletion {
		t.Fatal("exit completion and correction route does not round-trip")
	}
	doc, err := Render(testView(PageExitCompletion))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("exit completion and correction exposes an unresolved message key")
	}
	for _, invented := range []string{"exit completed", "record corrected", "closed by Uma", "completion ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("exit completion and correction invents completion data: %q", invented)
		}
	}
}

// Golden: the registered exit completion definition
// and its fallback copy.
func TestTodo_WEB_216_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageExitCompletion)
	if !ok {
		t.Fatal("exit completion and correction unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("exit_completion.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("exit_completion.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "6615788c915db86ee70bb88189208aab70b93a23e673021c5532cf663c14c9f4"
	if got != want {
		t.Fatalf("exit completion and correction digest = %s, want %s", got, want)
	}
}

// Browser: exit completion and correction renders
// deterministically and round-trips its route.
func TestTodo_WEB_216_Browser(t *testing.T) {
	first, err := Render(testView(PageExitCompletion))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageExitCompletion))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("exit completion and correction renders nondeterministically")
	}
	definition, _ := LookupPage(PageExitCompletion)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageExitCompletion {
		t.Fatal("exit completion and correction route does not round-trip")
	}
}

// Conformance: exit completion and correction keeps the
// registry contract — visible to the manager, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_216_Conformance(t *testing.T) {
	if !PageVisible(PageExitCompletion, []string{"manager"}) {
		t.Fatal("exit completion and correction hidden from the manager")
	}
	if PageVisible(PageExitCompletion, nil) {
		t.Fatal("exit completion and correction visible without roles")
	}
	definition, _ := LookupPage(PageExitCompletion)
	if definition.PrimaryNav {
		t.Fatal("exit completion and correction claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageExitCompletion), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("exit completion and correction leaks a key in %s", code)
		}
	}
}
