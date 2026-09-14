package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-198: restricted case messaging. The
// registry owns every product surface, but no restricted
// messaging exists: exchanging case messages under
// authorization has no exposure point and the first
// surface invents message data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that messages
// nothing until the governed help service publishes,
// with message truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_198(t *testing.T) {
	definition, ok := LookupPage(PageCaseMessaging)
	if !ok {
		t.Fatal("restricted case messaging unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("restricted case messaging incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageCaseMessaging {
		t.Fatal("restricted case messaging route does not round-trip")
	}
	doc, err := Render(testView(PageCaseMessaging))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("restricted case messaging exposes an unresolved message key")
	}
	for _, invented := range []string{"agent replied kindly", "2 unread messages", "sent by Farah", "messaging ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("restricted case messaging invents message data: %q", invented)
		}
	}
}

// Golden: the registered restricted messaging
// definition and its fallback copy.
func TestTodo_WEB_198_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageCaseMessaging)
	if !ok {
		t.Fatal("restricted case messaging unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("case_messaging.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("case_messaging.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "16cb7d6f35c142e7d549b0dc6673e3c74cfb66a0f8b36b631a3dde28d45cd614"
	if got != want {
		t.Fatalf("restricted case messaging digest = %s, want %s", got, want)
	}
}

// Browser: restricted case messaging renders
// deterministically and round-trips its route.
func TestTodo_WEB_198_Browser(t *testing.T) {
	first, err := Render(testView(PageCaseMessaging))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageCaseMessaging))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("restricted case messaging renders nondeterministically")
	}
	definition, _ := LookupPage(PageCaseMessaging)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageCaseMessaging {
		t.Fatal("restricted case messaging route does not round-trip")
	}
}

// Conformance: restricted case messaging keeps the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_198_Conformance(t *testing.T) {
	if !PageVisible(PageCaseMessaging, []string{"worker_self"}) {
		t.Fatal("restricted case messaging hidden from the employee")
	}
	if PageVisible(PageCaseMessaging, nil) {
		t.Fatal("restricted case messaging visible without roles")
	}
	definition, _ := LookupPage(PageCaseMessaging)
	if definition.PrimaryNav {
		t.Fatal("restricted case messaging claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageCaseMessaging), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("restricted case messaging leaks a key in %s", code)
		}
	}
}
