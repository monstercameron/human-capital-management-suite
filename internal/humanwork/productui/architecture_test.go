package productui

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestRouteAdaptersDoNotOwnMarkup(t *testing.T) {
	files, err := filepath.Glob("page_*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imported := range parsed.Imports {
			name, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("unquote import in %s: %v", path, err)
			}
			if name == "github.com/monstercameron/GoWebComponents/v5/html" {
				t.Fatalf("%s imports html; route adapters must only shape props for feature components", path)
			}
		}
	}
}

func TestMoneyBearingViewFieldsUseTheExactMoneyValueObject(t *testing.T) {
	want := reflect.TypeOf(values.Money{})
	for _, field := range []struct {
		owner reflect.Type
		name  string
	}{
		{reflect.TypeOf(WorkItem{}), "CurrentBase"},
		{reflect.TypeOf(WorkItem{}), "ProposedBase"},
		{reflect.TypeOf(Person{}), "BasePay"},
	} {
		got, ok := field.owner.FieldByName(field.name)
		if !ok {
			t.Fatalf("%s.%s is missing", field.owner.Name(), field.name)
		}
		if got.Type != want {
			t.Fatalf("%s.%s has type %s, want exact %s (never float or an amount/currency string pair)", field.owner.Name(), field.name, got.Type, want)
		}
	}
}

func TestPageRegistryOwnsCanonicalIdentityRouteAndRenderer(t *testing.T) {
	definitions := PageDefinitions()
	if len(definitions) != 124 {
		t.Fatalf("page registry has %d definitions; want 124", len(definitions))
	}
	ids := map[PageID]bool{}
	routes := map[string]bool{}
	previousOrder := 0
	for _, definition := range definitions {
		if definition.ID == "" || definition.Route == "" || definition.Label == "" || definition.Icon == "" || definition.Title == "" || pageRenderer(definition.ID) == nil {
			t.Fatalf("incomplete page definition: %+v", definition)
		}
		if ids[definition.ID] {
			t.Fatalf("duplicate page identity %q", definition.ID)
		}
		if routes[definition.Route] {
			t.Fatalf("duplicate page route %q", definition.Route)
		}
		if definition.RenderOrder <= previousOrder {
			t.Fatalf("page %q is out of stable render order", definition.ID)
		}
		ids[definition.ID], routes[definition.Route] = true, true
		previousOrder = definition.RenderOrder

		resolved, ok := LookupRoute(definition.Route)
		if !ok || resolved.ID != definition.ID {
			t.Fatalf("route %q does not round-trip", definition.Route)
		}
		if _, err := Render(testView(definition.ID)); err != nil {
			t.Fatalf("registered page %q does not render: %v", definition.ID, err)
		}
	}
}

func TestPageDefinitionInventoryCannotBeMutatedByCaller(t *testing.T) {
	first := PageDefinitions()
	first[0].Route = "/corrupted"
	second := PageDefinitions()
	if second[0].Route == "/corrupted" {
		t.Fatal("page registry escaped as mutable shared state")
	}
}

func TestAddressStateAppliesWithoutInventingRecords(t *testing.T) {
	view := NewView(PagePeople, "tenant", "principal", "scope")
	view = ApplyRequest(view, PageRequest{
		Page: PagePeople, Query: " Product ", WorkflowQuery: " Promotion ", SelectedPerson: "avery", Mode: "preview", NavCollapsed: true,
	})
	if view.Query != "Product" || view.PeoplePage != 1 || view.WorkflowQuery != "Promotion" || view.Mode != "preview" || view.SelectedPerson != "avery" || !view.NavCollapsed {
		t.Fatalf("request state was not normalized: %+v", view)
	}
	if len(view.Work) != 0 || len(view.People) != 0 {
		t.Fatal("presentation state invented business records")
	}
}

func TestCanonicalJourneyWorkerReferenceReopensTheExactProfile(t *testing.T) {
	view := NewView(PagePerson, "tenant", "principal", "scope")
	view.People = []Person{{
		ID: "noor-haddad", WorkerID: "d83a9e3a-2762-4a5c-a22f-3f37c7711c43", Name: "Noor Haddad",
	}}

	view = ApplyRequest(view, PageRequest{
		Page: PagePerson, SelectedPerson: "eref:v1:harborcare-demo:WORKER:d83a9e3a-2762-4a5c-a22f-3f37c7711c43",
	})

	if view.SelectedPerson != "noor-haddad" {
		t.Fatalf("canonical journey subject resolved to %q, want Noor's stable profile key", view.SelectedPerson)
	}
}

func TestShellAndFeatureCompositionRemainSeparate(t *testing.T) {
	doc, err := Render(testView(PageHome))
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range []string{`class="topbar"`, `class="sidebar"`, `id="main-content"`, `class="home-grid"`} {
		if !strings.Contains(doc, contract) {
			t.Fatalf("composed document missing %s", contract)
		}
	}
}

func TestPageContentCanRenderWithoutOwningApplicationChrome(t *testing.T) {
	content, err := ui.RenderToString(BuildPageContent(testView(PageHome)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, `class="home-grid"`) {
		t.Fatal("feature content did not render its route body")
	}
	for _, shellNode := range []string{`class="app-shell`, `class="topbar"`, `class="sidebar"`, `id="main-content"`} {
		if strings.Contains(content, shellNode) {
			t.Fatalf("feature content unexpectedly owns persistent shell node %s", shellNode)
		}
	}
}

func TestAppLinkInstallsSoftwareNavigationHandler(t *testing.T) {
	view := NewView(PageHome, "tenant-test", "Taylor", "manager")
	view.Navigate = func(string) {}
	node := appLink(view, html.Props{}, "/workspace/app/people", ui.Text("People"))
	if node == nil || node.Props["onclick"] == nil {
		t.Fatal("internal product link did not install a Go/WASM navigation handler")
	}

	external := appLink(view, html.Props{}, "/workspace/journey#/journeys", ui.Text("Journey"))
	if external != nil && external.Props["onclick"] != nil {
		t.Fatal("cross-application link must remain browser-owned")
	}
}
