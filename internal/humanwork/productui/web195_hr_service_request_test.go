package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-195: HR service-request intake. The
// registry owns every product surface, but no HR intake
// exists: raising an HR service request has no exposure
// point and the first surface invents intake data by
// convention. The compiler needs the registered surface
// — canonical identity, route, and an honest fallback
// that intakes nothing until the governed help service
// publishes, with request truth staying server authority
// — so the surface resolves today without a second
// source of business authority.
func TestTodo_WEB_195(t *testing.T) {
	definition, ok := LookupPage(PageHRServiceRequest)
	if !ok {
		t.Fatal("HR service-request intake unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("HR service-request intake incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageHRServiceRequest {
		t.Fatal("HR service-request intake route does not round-trip")
	}
	doc, err := Render(testView(PageHRServiceRequest))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("HR service-request intake exposes an unresolved message key")
	}
	for _, invented := range []string{"request 4471 filed", "pay correction pending", "assigned to HR Ana", "intake ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("HR service-request intake invents intake data: %q", invented)
		}
	}
}

// Golden: the registered HR intake definition and its
// fallback copy.
func TestTodo_WEB_195_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageHRServiceRequest)
	if !ok {
		t.Fatal("HR service-request intake unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("hr_service_request.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("hr_service_request.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "f2df5d6e554ffb8f9795c002f3cdb3cf251194ddb9fe0e90aea8a126f1fda549"
	if got != want {
		t.Fatalf("HR service-request intake digest = %s, want %s", got, want)
	}
}

// Browser: HR service-request intake renders
// deterministically and round-trips its route.
func TestTodo_WEB_195_Browser(t *testing.T) {
	first, err := Render(testView(PageHRServiceRequest))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageHRServiceRequest))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("HR service-request intake renders nondeterministically")
	}
	definition, _ := LookupPage(PageHRServiceRequest)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageHRServiceRequest {
		t.Fatal("HR service-request intake route does not round-trip")
	}
}

// Conformance: HR service-request intake keeps the
// registry contract — visible to the employee, hidden
// from the role-less baseline, ordered, and honest in
// every locale.
func TestTodo_WEB_195_Conformance(t *testing.T) {
	if !PageVisible(PageHRServiceRequest, []string{"worker_self"}) {
		t.Fatal("HR service-request intake hidden from the employee")
	}
	if PageVisible(PageHRServiceRequest, nil) {
		t.Fatal("HR service-request intake visible without roles")
	}
	definition, _ := LookupPage(PageHRServiceRequest)
	if definition.PrimaryNav {
		t.Fatal("HR service-request intake claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageHRServiceRequest), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("HR service-request intake leaks a key in %s", code)
		}
	}
}
