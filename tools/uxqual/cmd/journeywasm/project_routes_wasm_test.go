//go:build js && wasm

package main

import (
	"sync/atomic"
	"syscall/js"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/router"
	renderfixture "github.com/monstercameron/GoWebComponents/v5/testkit/render"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/projectclient"
)

func TestTodo_PM_029_Browser(t *testing.T) {
	initial := projectclient.CanonicalHref(projectclient.State{Route: projectclient.RouteProjects})
	browser := installWASMHistory(t, initial)
	var reloads atomic.Int32
	browser.addCallback("reload", func(js.Value, []js.Value) any {
		reloads.Add(1)
		return nil
	}, browser.location)

	productRouter := router.NewHistoryRouter(router.RouterOptions{DefaultRoute: projectclient.ProjectsPath})
	productRouter.SetFocusManagement(false)
	productRouter.SetViewTransitions(false)
	for _, path := range []string{projectclient.ProjectsPath, projectclient.ProjectPath} {
		path := path
		productRouter.Register(path, func(router.Attrs) *router.Element { return html.Section(html.Props{ID: "project-route"}) })
	}
	productRouter.Navigate(initial)
	if productRouter.Current() == nil {
		t.Fatal("project home did not resolve in the browser HistoryRouter")
	}

	fixture := renderfixture.New(t)
	view := productui.NewView(productui.PageProjects, "Projects", "", "")
	view.ProjectsState = productui.ProjectProjectionReady
	view.Projects = []productui.ProjectSummaryProjection{{ID: "p-opaque_1", Name: "Launch"}}
	view.Navigate = productRouter.Navigate
	fixture.Render(ui.CreateElement(renderProjectRouteTestHome, view))
	open := fixture.ByRole("link", "Open project")
	if open == nil {
		t.Fatal("authorized project row has no open link")
	}
	open.Click()
	wantBoard := projectclient.CanonicalHref(projectclient.State{Route: projectclient.RouteProject, ProjectID: "p-opaque_1"})
	if got := currentProductHref(); got != wantBoard {
		t.Fatalf("project row navigation URL = %q, want %q", got, wantBoard)
	}
	if reloads.Load() != 0 {
		t.Fatalf("project row navigation reloaded the document %d times", reloads.Load())
	}

	browser.history.Call("go", -1)
	if got := currentProductHref(); got != initial {
		t.Fatalf("Back restored %q, want %q", got, initial)
	}
	if productRouter.Current() == nil {
		t.Fatal("Back left no current project route")
	}
	browser.history.Call("go", 1)
	if got := currentProductHref(); got != wantBoard {
		t.Fatalf("Forward restored %q, want %q", got, wantBoard)
	}
	if reloads.Load() != 0 {
		t.Fatalf("Back/Forward reloaded the document %d times", reloads.Load())
	}
	fixture.Cleanup()
}

func renderProjectRouteTestHome(view productui.View) ui.Node {
	return productui.BuildPageContent(view)
}
