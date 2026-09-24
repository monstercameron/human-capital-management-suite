//go:build js && wasm

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/router"
	renderfixture "github.com/monstercameron/GoWebComponents/v5/testkit/render"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type rev06301BrowserRouteProps struct {
	View productui.View
}

func rev06301BrowserRoute(props rev06301BrowserRouteProps) ui.Node {
	return productui.BuildShell(props.View, productui.BuildPageContent(props.View), true)
}

func TestTodo_REV_063_01_Browser(t *testing.T) {
	const href = "/workspace/app/people?locale=ar"
	installWASMHistory(t, href)
	fixture := renderfixture.New(t)
	defer fixture.Cleanup()

	oldFocusedRoute := lastFocusedProductRoute
	lastFocusedProductRoute = ""
	t.Cleanup(func() { lastFocusedProductRoute = oldFocusedRoute })

	loaded := make(chan productui.View, 1)
	service := productclient.Service{
		ListJourneys: func(context.Context, *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
			return nil, status.Error(codes.Unavailable, "journey projection unavailable")
		},
		ListWorkers: func(context.Context, *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
			return nil, status.Error(codes.Unauthenticated, "expired bearer credential")
		},
	}
	productRouter := router.NewHistoryRouter(router.RouterOptions{DefaultRoute: productui.Path(productui.PagePeople)})
	productRouter.SetFocusManagement(false)
	productRouter.SetViewTransitions(false)
	productRouter.Register(productui.Path(productui.PagePeople), func(attrs router.Attrs) *router.Element {
		view, _ := attrs[productViewKey].(productui.View)
		return ui.CreateElement(rev06301BrowserRoute, rev06301BrowserRouteProps{View: view})
	}, router.Options{Loader: func(ctx context.Context, routeContext router.RouteContext) (router.Attrs, error) {
		state, err := productclient.ParseState(routeContext.Path, routeContext.Query.Encode())
		if err != nil {
			return nil, err
		}
		session := productclient.Session{Tenant: "tenant-a", Principal: "worker-a"}
		view, loadErr := productclient.Load(ctx, service, session, state)
		view, regionFailed := productRouteFailure(view, state.Page, loadErr)
		if regionFailed {
			t.Errorf("unauthenticated route selected async-region retry instead of shell recovery")
		}
		loaded <- view
		return router.Attrs{productViewKey: view}, nil
	}})
	initial := productRouter.Current()
	if initial == nil {
		t.Fatal("HistoryRouter did not resolve the product route")
	}
	if got := router.InspectCurrentRoute().Path; got != productui.Path(productui.PagePeople) {
		t.Fatalf("current router path = %q", got)
	}
	fixture.Render(initial)
	var view productui.View
	select {
	case view = <-loaded:
	case <-time.After(3 * time.Second):
		t.Fatal("route loader did not publish the authentication refusal")
	}
	if view.SignedOut == nil || view.Locale.Resolved != "ar" {
		t.Fatalf("loaded route projection = (signed-out %v, locale %q)", view.SignedOut != nil, view.Locale.Resolved)
	}
	fixture.Stabilize()
	fixture.Rerender(productRouter.Current())
	fixture.Stabilize()

	main := fixture.ByID("main-content")
	panel := fixture.ByID("signed-out")
	title := fixture.ByID("signed-out-title")
	if main == nil || panel == nil || title == nil {
		t.Fatalf("mounted product shell is missing main/panel/title (main=%v panel=%v title=%v)", main != nil, panel != nil, title != nil)
	}
	if panel.Attr("role") != "alert" || panel.Attr("aria-labelledby") != "signed-out-title" {
		t.Fatalf("mounted recovery panel semantics = role %q labelledby %q", panel.Attr("role"), panel.Attr("aria-labelledby"))
	}
	var signIn *renderfixture.QueryNode
	for _, link := range fixture.AllByTag("a") {
		if link.Attr("class") == "signed-out-signin" {
			signIn = link
			break
		}
	}
	if signIn == nil || signIn.Attr("href") != "/workspace/login" || signIn.Text() != productui.ResolveProductLocale("ar").Text("signed_out.signin") {
		t.Fatalf("mounted localized sign-in link = %+v", signIn)
	}
	content := fixture.Container().Text()
	if strings.Contains(content, "Try again") || strings.Contains(content, "expired bearer credential") || strings.Contains(content, "journey projection unavailable") {
		t.Fatalf("mounted route showed retry or leaked transport errors: %q", content)
	}
	if !strings.Contains(title.Text(), productui.ResolveProductLocale("ar").Text("signed_out.title")) {
		t.Fatalf("mounted title is not localized: %q", title.Text())
	}
}
