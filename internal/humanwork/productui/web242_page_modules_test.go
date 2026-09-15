package productui

import (
	"reflect"
	"strings"
	"testing"
)

func TestTodo_WEB_242(t *testing.T) {
	modules := PageModules()
	if len(modules) == 0 {
		t.Fatal("page module registry is empty")
	}
	if len(modules) != len(PageDefinitions()) {
		t.Fatalf("module/definition cardinality = %d/%d", len(modules), len(PageDefinitions()))
	}
	for _, module := range modules {
		definition, ok := LookupPage(module.Definition.ID)
		if !ok || definition.Route != module.Definition.Route {
			t.Fatalf("module %q does not own canonical definition lookup", module.Definition.ID)
		}
		byRoute, ok := LookupRouteModule(module.Definition.Route)
		if !ok || byRoute.Definition.ID != module.Definition.ID {
			t.Fatalf("module route %q does not round-trip", module.Definition.Route)
		}
		if renderer := pageRenderer(module.Definition.ID); renderer == nil {
			t.Fatalf("module %q does not own render dispatch", module.Definition.ID)
		}
		if !reflect.DeepEqual(module.Features, definition.Features) {
			t.Fatalf("module %q has divergent feature projections", module.Definition.ID)
		}
	}
}

func TestTodo_WEB_242_Property(t *testing.T) {
	first := PageModules()
	first[0].Definition.Route = "/corrupted"
	first[0].Definition.SearchTerms[0] = "corrupted"
	first[0].Definition.Features[0].ID = "corrupted"
	first[0].Features[0].ID = "also_corrupted"

	second := PageModules()
	if second[0].Definition.Route == "/corrupted" || second[0].Definition.SearchTerms[0] == "corrupted" ||
		second[0].Definition.Features[0].ID == "corrupted" || second[0].Features[0].ID == "also_corrupted" {
		t.Fatal("page module registry escaped as mutable shared state")
	}
}

func TestTodo_WEB_242_Security(t *testing.T) {
	cases := []struct {
		page  PageID
		roles []string
		want  bool
	}{
		{page: PageHome, want: true},
		{page: PagePortal, want: true},
		{page: PageMyself, roles: []string{"worker_self"}, want: true},
		{page: PageMyself, roles: []string{"finance_partner"}, want: true},
		{page: PageJourneys, roles: []string{"worker_self"}, want: false},
		{page: PageJourneys, roles: []string{"manager"}, want: true},
		{page: PageWork, roles: []string{"finance_partner"}, want: true},
		{page: PageLeaveEvidence, roles: []string{"manager"}, want: false},
		{page: PageLeaveEvidence, roles: []string{"hr_partner"}, want: true},
		{page: PageRoles, roles: []string{"manager"}, want: false},
		{page: PageRoles, roles: []string{RoleHCMAdmin}, want: true},
		{page: PageID("unknown"), roles: []string{RoleHCMAdmin}, want: false},
	}
	for _, test := range cases {
		if got := PageVisible(test.page, test.roles); got != test.want {
			t.Errorf("PageVisible(%q, %v) = %v, want %v", test.page, test.roles, got, test.want)
		}
	}
}

func TestTodo_WEB_242_Architecture(t *testing.T) {
	if err := ValidatePageRegistry(); err != nil {
		t.Fatal(err)
	}
	valid := PageModules()[0]
	cases := []struct {
		name   string
		mutate func(*PageModule)
		want   string
	}{
		{name: "empty id", mutate: func(module *PageModule) { module.Definition.ID = "" }, want: "empty ID"},
		{name: "empty route", mutate: func(module *PageModule) { module.Definition.Route = "" }, want: "empty route"},
		{name: "invalid route", mutate: func(module *PageModule) { module.Definition.Route = "/outside/product" }, want: "invalid product route"},
		{name: "identity", mutate: func(module *PageModule) { module.Definition.LabelKey = "" }, want: "incomplete identity"},
		{name: "renderer", mutate: func(module *PageModule) { module.Render = nil }, want: "no renderer"},
		{name: "access", mutate: func(module *PageModule) { module.Access.Audience = "invented" }, want: "invalid access audience"},
		{name: "features", mutate: func(module *PageModule) { module.Features = nil }, want: "no features"},
		{name: "feature id", mutate: func(module *PageModule) { module.Features[0].ID = "Not Stable" }, want: "invalid feature ID"},
		{name: "feature ceiling", mutate: func(module *PageModule) { module.Features[0].View = false; module.Features[0].Update = true }, want: "view ceiling"},
		{name: "route profile", mutate: func(module *PageModule) { module.RouteProfile = "" }, want: "no route profile"},
		{name: "data profile", mutate: func(module *PageModule) { module.DataProfile = "" }, want: "no data profile"},
		{name: "feature catalogue", mutate: func(module *PageModule) { module.Definition.Features[0].Label = "diverged" }, want: "divergent feature catalogue"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			candidate := clonePageModule(valid)
			test.mutate(&candidate)
			err := ValidatePageModules([]PageModule{candidate})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidatePageModules error = %v, want %q", err, test.want)
			}
		})
	}
	duplicate := clonePageModule(valid)
	duplicate.Definition.RenderOrder++
	if err := ValidatePageModules([]PageModule{valid, duplicate}); err == nil || !strings.Contains(err.Error(), "duplicate page module ID") {
		t.Fatalf("duplicate module validation = %v", err)
	}
	parent := clonePageModule(valid)
	parent.Definition.ParentNav = PageID("missing-parent")
	if err := ValidatePageModules([]PageModule{parent}); err == nil || !strings.Contains(err.Error(), "unknown navigation parent") {
		t.Fatalf("parent validation = %v", err)
	}
}

func TestTodo_WEB_242_Regression(t *testing.T) {
	for _, module := range PageModules() {
		view := testView(module.Definition.ID)
		content := BuildPageContent(view)
		if content == nil {
			t.Fatalf("module %q returned nil content", module.Definition.ID)
		}
		resolved, ok := LookupRoute(module.Definition.Route)
		if !ok || resolved.ID != module.Definition.ID {
			t.Fatalf("module %q route regression", module.Definition.ID)
		}
	}
}
