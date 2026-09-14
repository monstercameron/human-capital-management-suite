package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-168: payroll and benefit reconciliation
// status. The registry owns every product surface, but no
// reconciliation status exists: knowing whether payroll
// and benefits agree has no exposure point and the first
// surface invents reconciliation state by convention. The
// compiler needs the registered surface — canonical
// identity, route, and an honest fallback that reports
// nothing until the governed reconciliation service
// publishes, with reconciliation truth staying server
// authority — so the surface resolves today without a
// second source of business authority.
func TestTodo_WEB_168(t *testing.T) {
	definition, ok := LookupPage(PagePayBenefitRecon)
	if !ok {
		t.Fatal("payroll and benefit reconciliation status unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("payroll and benefit reconciliation status incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PagePayBenefitRecon {
		t.Fatal("reconciliation status route does not round-trip")
	}
	doc, err := Render(testView(PagePayBenefitRecon))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("reconciliation status exposes an unresolved message key")
	}
	for _, invented := range []string{"reconciled:", "3 breaks", "matched:", "clean ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("reconciliation status invents reconciliation data: %q", invented)
		}
	}
}

// Golden: the registered reconciliation status definition
// and its fallback copy.
func TestTodo_WEB_168_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PagePayBenefitRecon)
	if !ok {
		t.Fatal("payroll and benefit reconciliation status unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("pay_benefit_recon.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("pay_benefit_recon.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	const want = "77e714e4e5935fd7acf724fcec1b3f80b0222c659d63d975e179814e07ddb241"
	if got != want {
		t.Fatalf("reconciliation status digest = %s, want %s", got, want)
	}
}

// Browser: reconciliation status renders deterministically
// and round-trips its route.
func TestTodo_WEB_168_Browser(t *testing.T) {
	first, err := Render(testView(PagePayBenefitRecon))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PagePayBenefitRecon))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("reconciliation status renders nondeterministically")
	}
	definition, _ := LookupPage(PagePayBenefitRecon)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PagePayBenefitRecon {
		t.Fatal("reconciliation status route does not round-trip")
	}
}

// Conformance: reconciliation status keeps the registry
// contract — visible to managers, hidden from the
// role-less baseline, ordered, and honest in every
// locale.
func TestTodo_WEB_168_Conformance(t *testing.T) {
	if !PageVisible(PagePayBenefitRecon, []string{"manager"}) {
		t.Fatal("reconciliation status hidden from managers")
	}
	if PageVisible(PagePayBenefitRecon, nil) {
		t.Fatal("reconciliation status visible without roles")
	}
	definition, _ := LookupPage(PagePayBenefitRecon)
	if definition.PrimaryNav {
		t.Fatal("reconciliation status claims primary navigation before its service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PagePayBenefitRecon), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("reconciliation status leaks a key in %s", code)
		}
	}
}
