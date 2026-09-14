//go:build js && wasm

package journey

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	gwctest "github.com/monstercameron/GoWebComponents/v5/testkit/render"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func TestJourneyLoadCompletesBeforeSubscription(t *testing.T) {
	// Initialize the UI facade before the testkit installs its runtime.
	_ = ui.RenderInto(nil, nil)
	fixture := gwctest.New(t)
	store := NewStore(Page{Title: "Loading journeys"})
	completed := false
	fixture.Render(liveComponent(store, func(page Page) ui.Node {
		if !completed {
			completed = true
			// Force the response into the read/subscribe gap.
			store.Set(Page{Title: "Journeys ready"})
		}
		return html.Div(html.Props{}, ui.Text(page.Title))
	}))
	fixture.Flush()
	if fixture.Text() != "Journeys ready" {
		t.Fatalf("stuck page: %q", fixture.Text())
	}
	if store.SubscriberCount() != 1 {
		t.Fatal("missing or duplicate subscription")
	}
	store.Set(Page{Title: "Later update"})
	fixture.Flush()
	if fixture.Text() != "Later update" {
		t.Fatal("later updates stopped rendering")
	}
	fixture.Rerender(nil)
	fixture.Flush()
	if store.SubscriberCount() != 0 {
		t.Fatal("unmount leaked subscription")
	}
}

func TestJourneyRemountCatchesAnswerBetweenReadAndSubscription(t *testing.T) {
	fixture := gwctest.New(t)
	store := NewStore(Page{Title: "First visit"})
	build := func(page Page) ui.Node { return html.Div(html.Props{}, ui.Text(page.Title)) }
	fixture.Render(liveComponent(store, build))
	fixture.Flush()
	fixture.Rerender(nil)
	fixture.Flush()
	if store.SubscriberCount() != 0 {
		t.Fatal("leaving Journeys retained its subscriber")
	}

	store.Set(Page{Title: "Loading second visit"})
	answered := false
	fixture.Rerender(liveComponent(store, func(page Page) ui.Node {
		if !answered {
			answered = true
			store.Set(Page{Title: "Second visit ready"})
		}
		return build(page)
	}))
	fixture.Flush()
	if got := fixture.Text(); got != "Second visit ready" {
		t.Fatalf("remounted page missed its response: %q", got)
	}
	if store.SubscriberCount() != 1 {
		t.Fatalf("remounted subscriber count = %d, want 1", store.SubscriberCount())
	}
}

func TestJourneyBurstOfStoreUpdatesRendersLastAnswer(t *testing.T) {
	fixture := gwctest.New(t)
	store := NewStore(Page{Title: "Initial"})
	fixture.Render(liveComponent(store, func(page Page) ui.Node {
		return html.Div(html.Props{}, ui.Text(page.Title))
	}))
	fixture.Flush()
	store.Set(Page{Title: "Loading"})
	store.Set(Page{Title: "Promotion ready"})
	fixture.Flush()
	if got := fixture.Text(); got != "Promotion ready" {
		t.Fatalf("batched notifications left stale content: %q", got)
	}
}

func TestTodo_PROMOUX_010_Regression_PendingReviewDoesNotReconcileOpenDialog(t *testing.T) {
	fixture := gwctest.New(t)
	page := SampleDetailPage()
	page.Title = "Review is open"
	store := NewStore(page)
	fixture.Render(liveComponent(store, func(current Page) ui.Node {
		return html.Div(html.Props{}, ui.Text(current.Title))
	}))
	fixture.Flush()

	// The pending notice is rendered inside the browser-owned dialog. A
	// store revision at this point must not reconcile that dialog away.
	page.Title = "Review pending"
	page.Notice = &Notice{Tone: toneInfo, Busy: true, MessageKey: "journey.busy_approve"}
	store.Set(page)
	fixture.Flush()
	if got := fixture.Text(); got != "Review is open" {
		t.Fatalf("pending RPC reconciled away the open review: %q", got)
	}

	page.Title = "Approval recorded"
	page.Notice = &Notice{Tone: toneSuccess, Title: "Approval recorded"}
	store.Set(page)
	fixture.Flush()
	if got := fixture.Text(); got != "Approval recorded" {
		t.Fatalf("resolved RPC did not render its result: %q", got)
	}
}

func TestJourneyRouteKeyDoesNotRetainPreviousLoadingFiber(t *testing.T) {
	fixture := gwctest.New(t)
	store := NewStore(Page{Title: "Promotion form"})
	build := func(page Page) ui.Node { return html.Div(html.Props{}, ui.Text(page.Title)) }
	proposal := liveComponent(store, build)
	proposal.Props["key"] = "proposal"
	fixture.Render(proposal)
	fixture.Flush()

	store.Set(Page{Title: "Loading requests"})
	list := liveComponent(store, build)
	list.Props["key"] = "list"
	fixture.Rerender(list)
	store.Set(Page{Title: "Five requests"})
	fixture.Flush()
	if got := fixture.Text(); got != "Five requests" {
		t.Fatalf("new route retained a stale loading fiber: %q", got)
	}
	if store.SubscriberCount() != 1 {
		t.Fatalf("route switch has %d subscribers, want one", store.SubscriberCount())
	}
}

// These tests carry the same build tag as mount_wasm.go so
// `GOOS=js GOARCH=wasm go vet ./tools/uxqual/render/journey/` type-checks
// the product entrypoint with its test alongside it.
//
// The GWC testkit installs the js/wasm runtime with a deterministic DOM and
// scheduler. That makes TestTodo_WEB_027_Browser an actual live mount/update/
// unmount oracle without Playwright or a second renderer.

func TestTodo_WEB_027_Browser(t *testing.T) {
	fixture := gwctest.New(t)
	store := NewStore(SampleListPage())
	lifecycle := NewMountLifecycle()
	if err := lifecycle.Mount(RootSelector, func() error {
		fixture.Render(LiveComponent(store))
		return nil
	}, func() { fixture.Rerender(nil) }); err != nil {
		t.Fatal(err)
	}
	if store.SubscriberCount() != 1 {
		t.Fatalf("mounted subscriber count=%d, want 1", store.SubscriberCount())
	}
	if got := len(fixture.AllByTag("main")); got != 1 {
		t.Fatalf("main landmarks=%d, want 1", got)
	}
	if got := len(fixture.AllByTag("h1")); got != 1 {
		t.Fatalf("h1 count=%d, want 1", got)
	}
	if fixture.ByLiveRegion("polite", "") == nil {
		t.Fatal("mounted DOM has no polite live region")
	}

	store.Set(SampleDetailPage())
	fixture.Flush()
	if !strings.Contains(fixture.Text(), "Omar Reyes") {
		t.Fatalf("live store update did not reach DOM: %q", fixture.Text())
	}

	lifecycle.Stop()
	fixture.Flush()
	if store.SubscriberCount() != 0 {
		t.Fatalf("unmounted subscriber count=%d, want 0", store.SubscriberCount())
	}
	if children := fixture.Container().Children(); len(children) != 0 {
		t.Fatalf("unmount left %d DOM children", len(children))
	}
}

func TestMountBrowserDOMPreservesLocalizedAccessibleText(t *testing.T) {
	fixture := gwctest.New(t)
	page := SampleListPage()
	page.Brand = "موارد بشرية"
	page.Nav[0].Label = "رحلات الترقية"
	page.Notice.Title = "تم تسجيل الاقتراح"
	store := NewStore(page)
	fixture.Render(LiveComponent(store))

	if fixture.ByText("موارد بشرية") == nil {
		t.Fatal("localized brand text missing from live DOM")
	}
	if fixture.ByRole("link", "رحلات الترقية") == nil {
		t.Fatal("localized navigation link has no accessible name")
	}
	if fixture.ByLiveRegion("polite", "") == nil {
		t.Fatal("localized notice is not exposed as a live region")
	}
	fixture.Rerender(nil)
	fixture.Flush()
	if store.SubscriberCount() != 0 {
		t.Fatalf("localized tree leaked %d subscribers", store.SubscriberCount())
	}
}

func TestMountBuildsTheSameTreeTheNativeTestsAssertAgainst(t *testing.T) {
	for name, page := range samplePages() {
		t.Run(name, func(t *testing.T) {
			if Build(page) == nil {
				t.Fatal("Build returned a nil node; Mount would render nothing")
			}
			if LiveComponent(NewStore(page)) == nil {
				t.Fatal("LiveComponent returned a nil node; MountLive would render nothing")
			}
		})
	}
}

func TestMountInjectsExactlyTheHashedStylesheet(t *testing.T) {
	css := Stylesheet()
	if css == "" {
		t.Fatal("Stylesheet() is empty")
	}
	if strings.Contains(css, "</style") {
		t.Fatal("stylesheet closes its own style element; injecting it would break the head")
	}
}

// TestMountEntrypointsShareOneStore keeps Mount an alias for MountLive
// rather than a second, divergent mounting path.
func TestMountEntrypointsShareOneStore(t *testing.T) {
	s := NewStore(SampleListPage())
	if s.Page().List == nil {
		t.Fatal("the store did not keep the page it was seeded with")
	}
	s.Set(SampleDetailPage())
	if s.Page().Detail == nil {
		t.Error("the store did not follow a Set")
	}
}
