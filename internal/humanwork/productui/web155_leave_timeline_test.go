package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-155: leave-status timeline. The registry
// owns every product surface, but no leave timeline
// exists: following a leave case has no exposure point
// and the first surface invents case history by
// convention. The compiler needs the registered surface —
// canonical identity, route, and an honest fallback that
// shows nothing until the governed leave service
// publishes, with case truth staying server authority —
// so the surface resolves today without a second source
// of business authority.
func TestTodo_WEB_155(t *testing.T) {
	definition, ok := LookupPage(PageLeaveTimeline)
	if !ok {
		t.Fatal("leave-status timeline unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("leave-status timeline incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageLeaveTimeline {
		t.Fatal("leave timeline route does not round-trip")
	}
	doc, err := Render(testView(PageLeaveTimeline))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("leave timeline exposes an unresolved message key")
	}
	for _, invented := range []string{"timeline:", "opened:", "approved:", "closed ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("leave timeline invents case history: %q", invented)
		}
	}
}

// Golden: the registered leave timeline definition and
// its fallback copy.
func TestTodo_WEB_155_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageLeaveTimeline)
	if !ok {
		t.Fatal("leave-status timeline unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("leave_timeline.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("leave_timeline.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "21e702083603d6a420cb08b4575e18fb57eb9e80bb04dfea8d2702f4692805c9"
	if got != want {
		t.Fatalf("leave timeline digest = %s, want %s", got, want)
	}
}

// Browser: the leave timeline renders deterministically
// and round-trips its route.
func TestTodo_WEB_155_Browser(t *testing.T) {
	first, err := Render(testView(PageLeaveTimeline))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageLeaveTimeline))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("leave timeline renders nondeterministically")
	}
	definition, _ := LookupPage(PageLeaveTimeline)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageLeaveTimeline {
		t.Fatal("leave timeline route does not round-trip")
	}
}

// Conformance: the leave timeline keeps the registry
// contract — visible to the employee, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_155_Conformance(t *testing.T) {
	if !PageVisible(PageLeaveTimeline, []string{"worker_self"}) {
		t.Fatal("leave timeline hidden from the employee")
	}
	if PageVisible(PageLeaveTimeline, nil) {
		t.Fatal("leave timeline visible without roles")
	}
	definition, _ := LookupPage(PageLeaveTimeline)
	if definition.PrimaryNav {
		t.Fatal("leave timeline claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageLeaveTimeline), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("leave timeline leaks a key in %s", code)
		}
	}
}
