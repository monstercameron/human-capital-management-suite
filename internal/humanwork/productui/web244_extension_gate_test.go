package productui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestTodo_WEB_244(t *testing.T) {
	modules := PageModules()
	if err := ValidatePageModules(modules); err != nil {
		t.Fatal(err)
	}
	for _, module := range modules {
		routeProfile, dataProfile, ok := PageProfiles(module.Definition.ID)
		if !ok || routeProfile != module.RouteProfile || dataProfile != module.DataProfile {
			t.Fatalf("page %q does not resolve its declared profiles", module.Definition.ID)
		}
		page, byRoute, routeData, ok := RouteProfiles(module.Definition.Route)
		if !ok || page != module.Definition.ID || byRoute != routeProfile || routeData != dataProfile {
			t.Fatalf("route %q does not resolve its complete module contract", module.Definition.Route)
		}
		if BuildPageContent(testView(module.Definition.ID)) == nil {
			t.Fatalf("page %q returned no component outlet", module.Definition.ID)
		}
	}
}

func TestTodo_WEB_244_Golden(t *testing.T) {
	module, ok := LookupPageModule(PagePeople)
	if !ok {
		t.Fatal("representative People module is missing")
	}
	got := fmt.Sprintf("%s|%s|%s|%s|%s|%t|%t",
		module.Definition.ID, module.Definition.Route, module.Access.Audience,
		module.RouteProfile, module.DataProfile,
		module.Definition.NavigationPublished, module.Definition.Admitted,
	)
	const want = "people|/workspace/app/people|manager|people|journeys_workers|true|true"
	if got != want {
		t.Fatalf("representative module contract = %q, want %q", got, want)
	}
}

func TestTodo_WEB_244_Security(t *testing.T) {
	roles := []string{"worker_self"}
	view := ApplyRoleVisibility(testView(PageHome), roles)
	for _, module := range PageModules() {
		if PageVisible(module.Definition.ID, roles) || !module.Definition.NavigationPublished {
			continue
		}
		if navigationContains(view.Navigation, module.Definition.ID) || navigationContains(view.NavigationSupport, module.Definition.ID) {
			t.Fatalf("denied page %q leaked into authorized navigation", module.Definition.ID)
		}
	}
	for _, result := range globalSearchItems(view) {
		if !strings.HasPrefix(result.ID, "page:") {
			continue
		}
		page := PageID(strings.TrimPrefix(result.ID, "page:"))
		if !PageVisible(page, roles) {
			t.Fatalf("denied page %q leaked into global search", page)
		}
	}
	if _, _, _, ok := RouteProfiles("/workspace/app/not-registered"); ok {
		t.Fatal("unknown route resolved a module profile")
	}
}

func TestTodo_WEB_244_Accessibility(t *testing.T) {
	for _, module := range PageModules() {
		document, err := Render(testView(module.Definition.ID))
		if err != nil {
			t.Fatalf("render %q: %v", module.Definition.ID, err)
		}
		for _, contract := range []string{`href="#main-content"`, `id="main-content"`, `<h1`, `lang="en-US"`} {
			if !strings.Contains(document, contract) {
				t.Fatalf("page %q lacks accessibility contract %s", module.Definition.ID, contract)
			}
		}
		if strings.Count(document, `id="main-content"`) != 1 || strings.Count(document, "<h1") != 1 {
			t.Fatalf("page %q does not expose one main target and one h1", module.Definition.ID)
		}
	}
}

func TestTodo_WEB_244_Performance(t *testing.T) {
	if allocations := testing.AllocsPerRun(1_000, func() {
		_, _, ok := PageProfiles(PagePeople)
		if !ok {
			panic("page profile lookup failed")
		}
	}); allocations != 0 {
		t.Fatalf("page profile lookup allocations = %.0f, want 0", allocations)
	}
	if allocations := testing.AllocsPerRun(1_000, func() {
		_, _, _, ok := RouteProfiles("/workspace/app/people")
		if !ok {
			panic("route profile lookup failed")
		}
	}); allocations != 0 {
		t.Fatalf("route profile lookup allocations = %.0f, want 0", allocations)
	}
}

func TestTodo_WEB_244_Browser(t *testing.T) {
	document, err := Render(testView(PagePeople))
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range []string{
		`data-hcm-color-mode=`, `data-hcm-palette=`, `data-hcm-shape=`, `data-hcm-density=`,
		`data-hcm-glyphs=`, `data-hcm-typeface=`, `data-hcm-navigation=`, `data-hcm-motion=`,
	} {
		if !strings.Contains(document, contract) {
			t.Fatalf("browser document lacks inherited theme contract %q", contract)
		}
	}
	stylesheet := Stylesheet()
	for _, contract := range []string{"@media (max-width:760px)", "@media (max-width:420px)", `data-hcm-color-mode="dark"`, "prefers-reduced-motion"} {
		if !strings.Contains(stylesheet, contract) {
			t.Fatalf("browser stylesheet lacks responsive/theme contract %q", contract)
		}
	}
}

func TestTodo_WEB_244_Regression(t *testing.T) {
	pageCase := regexp.MustCompile(`(?m)\bcase\s+(?:productui\.)?Page[A-Z]`)
	for _, path := range []string{
		"provider.go",
		"shell.go",
		"../../../tools/uxqual/productclient/client.go",
		"../../../tools/uxqual/cmd/journeywasm/product_refresh.go",
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if match := pageCase.Find(content); match != nil {
			t.Fatalf("%s regrew page-ID dispatch %q instead of consuming module profiles", path, match)
		}
	}
}
