package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-156: return-to-work planning. The registry
// owns every product surface, but no return planning
// exists: planning a return from leave has no exposure
// point and the first surface invents return plans by
// convention. The compiler needs the registered surface —
// canonical identity, route, and an honest fallback that
// plans nothing until the governed leave service
// publishes, with plan truth staying server authority —
// so the surface resolves today without a second source
// of business authority.
func TestTodo_WEB_156(t *testing.T) {
	definition, ok := LookupPage(PageReturnToWork)
	if !ok {
		t.Fatal("return-to-work planning unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("return-to-work planning incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageReturnToWork {
		t.Fatal("return-to-work route does not round-trip")
	}
	doc, err := Render(testView(PageReturnToWork))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("return-to-work planning exposes an unresolved message key")
	}
	for _, invented := range []string{"plan:", "returns:", "2001-02-03", "planned ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("return-to-work planning invents plan data: %q", invented)
		}
	}
}

// Golden: the registered return planning definition and
// its fallback copy.
func TestTodo_WEB_156_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageReturnToWork)
	if !ok {
		t.Fatal("return-to-work planning unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("return_to_work.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("return_to_work.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "ca19efeed0dacdea856c02227b0cecaf32ec25d58617c7ff62ba2ad7b0c4e15c"
	if got != want {
		t.Fatalf("return-to-work digest = %s, want %s", got, want)
	}
}

// Browser: return planning renders deterministically and
// round-trips its route.
func TestTodo_WEB_156_Browser(t *testing.T) {
	first, err := Render(testView(PageReturnToWork))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageReturnToWork))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("return-to-work planning renders nondeterministically")
	}
	definition, _ := LookupPage(PageReturnToWork)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageReturnToWork {
		t.Fatal("return-to-work route does not round-trip")
	}
}

// Conformance: return planning keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_156_Conformance(t *testing.T) {
	if !PageVisible(PageReturnToWork, []string{"worker_self"}) {
		t.Fatal("return-to-work planning hidden from the employee")
	}
	if PageVisible(PageReturnToWork, nil) {
		t.Fatal("return-to-work planning visible without roles")
	}
	definition, _ := LookupPage(PageReturnToWork)
	if definition.PrimaryNav {
		t.Fatal("return-to-work planning claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageReturnToWork), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("return-to-work planning leaks a key in %s", code)
		}
	}
}
