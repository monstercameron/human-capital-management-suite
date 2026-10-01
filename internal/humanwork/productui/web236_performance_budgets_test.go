package productui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// RED for WEB-236: frontend performance budgets. The
// registry owns every product surface, but no budget
// surface exists: enforcing frontend performance budgets
// has no exposure point and the first surface invents
// budget data by convention. The compiler needs the
// registered surface — canonical identity, route, and an
// honest fallback that enforces nothing until the
// governed performance service publishes, with budget
// truth staying server authority — so the surface
// resolves today without a second source of business
// authority.
func TestTodo_WEB_236(t *testing.T) {
	definition, ok := LookupPage(PagePerformanceBudgets)
	if !ok {
		t.Fatal("frontend performance budgets unregistered")
	}
	if definition.Route == "" || pageRenderer(definition.ID) == nil {
		t.Fatalf("frontend performance budgets incomplete: %+v", definition)
	}
	roundTrip, ok := LookupRoute(definition.Route)
	if !ok || roundTrip.ID != PagePerformanceBudgets {
		t.Fatal("frontend performance budgets route does not round-trip")
	}
	doc, err := Render(testView(PagePerformanceBudgets))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc, "⟦") {
		t.Fatal("frontend performance budgets expose an unresolved message key")
	}
	for _, invented := range []string{"p95 under 800ms", "bundle 412KB", "budget met ✓", "budgets ✓"} {
		if strings.Contains(doc, invented) {
			t.Fatalf("frontend performance budgets invent budget data: %q", invented)
		}
	}
}

// Golden: the registered budget definition and its
// fallback copy.
func TestTodo_WEB_236_Golden(t *testing.T) {
	locale := ResolveProductLocale("en-US")
	definition, ok := LookupPage(PagePerformanceBudgets)
	if !ok {
		t.Fatal("frontend performance budgets unregistered")
	}
	golden := fmt.Sprintf("%s|%s|%s|%s|%s|%d\x00%s\x00%s\x00%s\x00%s\x00",
		definition.ID, definition.Route, definition.Label, definition.Title, definition.Subtitle, definition.RenderOrder,
		locale.Text(definition.LabelKey), locale.Text(definition.TitleKey), locale.Text(definition.SubtitleKey),
		locale.Text("performance_budgets.unavailable_title"))
	for _, code := range []string{"de-DE", "ar"} {
		other := ResolveProductLocale(code)
		golden += fmt.Sprintf("%s|%s|%s\x00", code, other.Text(definition.TitleKey), other.Text("performance_budgets.unavailable_title"))
	}
	digest := sha256.Sum256([]byte(golden))
	got := hex.EncodeToString(digest[:])
	// UXBLIND-027: page title now equals the page label (one name per page), which shifts every registry-derived golden digest.
	const want = "2e38a22edadb583d6602f13c1d25c82cb03bb12d3a6785e4987ad356ec6b8deb"
	if got != want {
		t.Fatalf("frontend performance budgets digest = %s, want %s", got, want)
	}
}

// Browser: frontend performance budgets render
// deterministically and round-trip their route.
func TestTodo_WEB_236_Browser(t *testing.T) {
	first, err := Render(testView(PagePerformanceBudgets))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(testView(PagePerformanceBudgets))
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("frontend performance budgets render nondeterministically")
	}
	definition, _ := LookupPage(PagePerformanceBudgets)
	resolved, ok := LookupRoute(definition.Route)
	if !ok || resolved.ID != PagePerformanceBudgets {
		t.Fatal("frontend performance budgets route does not round-trip")
	}
	live := testView(PagePerformanceBudgets)
	live.PerformanceBudgets = &PerformanceBudgetsProjection{Ready: true, Budgets: []PerformanceBudgetProjection{{ID: "browser-budget", Name: "startup", Target: "bounded"}}}
	firstLive, err := Render(live)
	if err != nil {
		t.Fatal(err)
	}
	secondLive, err := Render(live)
	if err != nil {
		t.Fatal(err)
	}
	if firstLive != secondLive {
		at := 0
		for at < len(firstLive) && at < len(secondLive) && firstLive[at] == secondLive[at] {
			at++
		}
		t.Fatalf("live performance budgets render is not deterministic at %d: %q vs %q", at, firstLive[max(0, at-40):min(len(firstLive), at+100)], secondLive[max(0, at-40):min(len(secondLive), at+100)])
	}
	if !strings.Contains(firstLive, "startup") {
		t.Fatal("live performance budget row missing")
	}
}

// Conformance: frontend performance budgets keep the
// registry contract — visible to the platform admin,
// hidden from the role-less baseline and the plain
// employee, ordered, and honest in every locale.
func TestTodo_WEB_236_Conformance(t *testing.T) {
	if !PageVisible(PagePerformanceBudgets, []string{RoleHCMAdmin}) {
		t.Fatal("frontend performance budgets hidden from the platform admin")
	}
	if PageVisible(PagePerformanceBudgets, nil) {
		t.Fatal("frontend performance budgets visible without roles")
	}
	if PageVisible(PagePerformanceBudgets, []string{"worker_self"}) {
		t.Fatal("frontend performance budgets visible to the plain employee")
	}
	definition, _ := LookupPage(PagePerformanceBudgets)
	if definition.PrimaryNav {
		t.Fatal("frontend performance budgets claim primary navigation before their service publishes")
	}
	for _, code := range SupportedProductLocales() {
		view := ApplyLocale(testView(PagePerformanceBudgets), ResolveProductLocale(code))
		doc, err := Render(view)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc, "⟦") {
			t.Fatalf("frontend performance budgets leak a key in %s", code)
		}
	}
}
