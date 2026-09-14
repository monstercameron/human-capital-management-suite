package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-230: the Policy Studio. The registry owns
// every product surface, but no Policy Studio exists:
// authoring governance policy has no exposure point and
// the first surface invents policy data by convention.
// The compiler needs the registered surface — canonical
// identity, route, and an honest fallback that authors
// nothing until the governed policy service publishes,
// with policy truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_230(t *testing.T) {
	definition, ok := LookupPage(PagePolicyStudio)
	if !ok {
		t.Fatal("Policy Studio unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("Policy Studio incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PagePolicyStudio {
		t.Fatal("Policy Studio route does not round-trip")
	}
	doc, err := Render(testView(PagePolicyStudio))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("Policy Studio exposes an unresolved message key")
	}
	for _, invented := range []string{"leave policy v9", "12 rules active", "edited by Sol", "policy ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("Policy Studio invents policy data: %q", invented)
		}
	}
}

// Golden: the registered Policy Studio definition and
// its fallback copy.
func TestTodo_WEB_230_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PagePolicyStudio)
	if !ok {
		t.Fatal("Policy Studio unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("policy_studio.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("policy_studio.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "e27212921cf683b66101b689dd2d706a843c3d93b4fe0f72546b49d58599e013"
	if got != want {
		t.Fatalf("Policy Studio digest = %s, want %s", got, want)
	}
}

// Browser: Policy Studio renders deterministically and
// round-trips its route.
func TestTodo_WEB_230_Browser(t *testing.T) {
	first, err := Render(testView(PagePolicyStudio))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PagePolicyStudio))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("Policy Studio renders nondeterministically")
	}
	definition, _ := LookupPage(PagePolicyStudio)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PagePolicyStudio {
		t.Fatal("Policy Studio route does not round-trip")
	}
}

// Conformance: Policy Studio keeps the registry
// contract — visible to the platform admin, hidden from
// the role-less baseline and the plain employee,
// ordered, and honest in every locale.
func TestTodo_WEB_230_Conformance(t *testing.T) {
	if !PageVisible(PagePolicyStudio, []string{RoleHCMAdmin}) {
		t.Fatal("Policy Studio hidden from the platform admin")
	}
	if PageVisible(PagePolicyStudio, nil) {
		t.Fatal("Policy Studio visible without roles")
	}
	if PageVisible(PagePolicyStudio, []string{"worker_self"}) {
		t.Fatal("Policy Studio visible to the plain employee")
	}
	definition, _ := LookupPage(PagePolicyStudio)
	if definition.PrimaryNav {
		t.Fatal("Policy Studio claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PagePolicyStudio), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("Policy Studio leaks a key in %s", code)
		}
	}
}
