package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-234: the reconciliation and repair
// workbench. The registry owns every product surface,
// but no repair workbench exists: reconciling and
// repairing governed data has no exposure point and the
// first surface invents repair data by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that repairs
// nothing until the governed repair service publishes,
// with repair truth staying server authority — so the
// surface resolves today without a second source of
// business authority.
func TestTodo_WEB_234(t *testing.T) {
	definition, ok := LookupPage(PageReconciliationWorkbench)
	if !ok {
		t.Fatal("reconciliation and repair workbench unregistered")
	}
	if definition.Route == "" || definition.render == nil {
		t.Fatalf("reconciliation and repair workbench incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PageReconciliationWorkbench {
		t.Fatal("reconciliation and repair workbench route does not round-trip")
	}
	doc, err := Render(testView(PageReconciliationWorkbench))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("reconciliation and repair workbench exposes an unresolved message key")
	}
	for _, invented := range []string{"9 breaks fixed", "ledger balanced", "repaired by Tilda", "workbench ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("reconciliation and repair workbench invents repair data: %q", invented)
		}
	}
}

// Golden: the registered repair workbench definition
// and its fallback copy.
func TestTodo_WEB_234_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PageReconciliationWorkbench)
	if !ok {
		t.Fatal("reconciliation and repair workbench unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("reconciliation_workbench.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("reconciliation_workbench.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "588bd09ba6b4233e86a56812d92a4009320f54d74842bcc6fd32ed60e0b8a80d"
	if got != want {
		t.Fatalf("reconciliation and repair workbench digest = %s, want %s", got, want)
	}
}

// Browser: reconciliation and repair workbench renders
// deterministically and round-trips its route.
func TestTodo_WEB_234_Browser(t *testing.T) {
	first, err := Render(testView(PageReconciliationWorkbench))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PageReconciliationWorkbench))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("reconciliation and repair workbench renders nondeterministically")
	}
	definition, _ := LookupPage(PageReconciliationWorkbench)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PageReconciliationWorkbench {
		t.Fatal("reconciliation and repair workbench route does not round-trip")
	}
}

// Conformance: reconciliation and repair workbench
// keeps the registry contract — visible to the platform
// admin, hidden from the role-less baseline and the
// plain employee, ordered, and honest in every locale.
func TestTodo_WEB_234_Conformance(t *testing.T) {
	if !PageVisible(PageReconciliationWorkbench, []string{RoleHCMAdmin}) {
		t.Fatal("reconciliation and repair workbench hidden from the platform admin")
	}
	if PageVisible(PageReconciliationWorkbench, nil) {
		t.Fatal("reconciliation and repair workbench visible without roles")
	}
	if PageVisible(PageReconciliationWorkbench, []string{"worker_self"}) {
		t.Fatal("reconciliation and repair workbench visible to the plain employee")
	}
	definition, _ := LookupPage(PageReconciliationWorkbench)
	if definition.PrimaryNav {
		t.Fatal("reconciliation and repair workbench claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PageReconciliationWorkbench), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("reconciliation and repair workbench leaks a key in %s", code)
		}
	}
}
