package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-138: interview scheduling. The registry
// owns every product surface, but no interview
// scheduling page exists: scheduling an interview has no
// exposure point and the first surface invents slots,
// interviewers, or confirmations by convention. The
// compiler needs the registered page — canonical
// identity, route, and an honest fallback that exposes
// no schedule until the governed service publishes — so
// scheduling resolves today without a second source of
// business authority.
func TestTodo_WEB_138(t *testing.T) {
	definition, ok := LookupPage(PageInterviews)
	if !ok {
		t.Fatal("interview scheduling unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("interview scheduling incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageInterviews {
		t.Fatal("interview route does not round-trip")
	}
	doc, err := Render(testView(PageInterviews))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("interview scheduling exposes an unresolved message key")
	}
	for _, invented := range []string{"10:00", "interviewers:", "confirmed:", "Room "} {
		if strings.Contains(doc, invented) {
			t.Fatalf("interview scheduling invents schedule data: %q", invented)
		}
	}
}

// Golden: the registered interview definition and its
// fallback copy.
func TestTodo_WEB_138_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageInterviews)
	if !ok {
		t.Fatal("interview scheduling unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("interviews.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("interviews.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "0f0f204eb73d39aebae56b9d1038a879b5d1f1d8c46f7f1553fefc06c8f15d1b"
	if got != want {
		t.Fatalf("interview digest = %s, want %s", got, want)
	}
}

// Browser: interview scheduling renders
// deterministically and round-trips its route.
func TestTodo_WEB_138_Browser(t *testing.T) {
	first, err := Render(testView(PageInterviews))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageInterviews))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("interview scheduling renders nondeterministically")
	}
	definition, _ := LookupPage(PageInterviews)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageInterviews {
		t.Fatal("interview route does not round-trip")
	}
}

// Conformance: interview scheduling keeps the registry
// contract — visible to hiring roles, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_138_Conformance(t *testing.T) {
	if !PageVisible(PageInterviews, []string{"hiring_manager"}) {
		t.Fatal("interview scheduling hidden from hiring managers")
	}
	if PageVisible(PageInterviews, nil) {
		t.Fatal("interview scheduling visible without roles")
	}
	definition, _ := LookupPage(PageInterviews)
	if definition.PrimaryNav {
		t.Fatal("interview scheduling claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageInterviews), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("interview scheduling leaks a key in %s", code)
		}
	}
}
