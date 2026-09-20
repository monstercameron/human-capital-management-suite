//go:build js && wasm

package main

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	renderfixture "github.com/monstercameron/GoWebComponents/v5/testkit/render"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// TestTodo_WEB_038_Browser runs inside the compiled Go js/wasm binary. It
// drives the real GWC click handler and state hooks, holds the authoritative
// answer in flight, and proves the old scoped projection remains intact until
// one complete replacement atomically clears/adopts without remounting chrome.
func TestTodo_WEB_038_Browser(t *testing.T) {
	initial := web038WASMView(productui.ContextSelection{TenantID: "tenant-a", ActingContextID: "self-a"})
	target := productui.ContextSelection{TenantID: "tenant-b", ActingContextID: "self-b"}
	started := make(chan struct{})
	release := make(chan struct{})
	committed := make(chan productui.ContextSelection, 1)
	controller := &productui.ContextSwitchController{}
	adapter := &web038WASMStagedAdapter{}
	exchange := func(ctx context.Context, selection productui.ContextSelection) (productui.ContextSwitchResult, error) {
		if selection != target {
			return productui.ContextSwitchResult{}, errors.New("foreign selection")
		}
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return productui.ContextSwitchResult{}, ctx.Err()
		}
		return adapter.stage(selection, web038WASMView(selection)), nil
	}
	scoped := &web038WASMScopedState{
		projection: "tenant-a/principal-a/self-a",
		history:    []string{"people", "settings"},
		storage:    "presentation-history-ledger",
	}

	fixture := renderfixture.New(t)
	fixture.Render(ui.CreateElement(web038WASMStableShell, web038WASMShellProps{
		Initial: initial, Controller: controller, Exchange: exchange, Adapter: adapter, Scoped: scoped, Committed: committed,
	}))
	marker := fixture.ByID("web038-shell-marker")
	navigation := fixture.ByID("workspace-navigation")
	main := fixture.ByID("main-content")
	banners := fixture.AllByTag("header")
	if marker == nil || navigation == nil || main == nil || len(banners) != 1 {
		t.Fatal("compiled GWC shell did not mount the stable chrome")
	}
	stableMarkerID, stableNavigationID, stableMainID, stableBannerID := marker.NodeID(), navigation.NodeID(), main.NodeID(), banners[0].NodeID()
	button := fixture.ByRole("button", "Switch to Northwind, Northwind employee")
	if button == nil || button.Attr("disabled") != "" {
		t.Fatalf("authorized pair button = %#v", button)
	}
	button.Click()
	wantWEB038WASMSignal(t, started, "authoritative exchange did not start")
	fixture.Stabilize()
	if scoped.projection != "tenant-a/principal-a/self-a" || len(scoped.history) != 2 || scoped.storage == "" {
		t.Fatalf("scoped state cleared before authoritative success: %+v", scoped)
	}
	if trigger := web038WASMSummary(fixture); trigger == nil || trigger.Text() != "HarborCare · Your own authority" {
		t.Fatalf("switcher claimed target while exchange was pending: text=%q node=%+v", trigger.Text(), trigger)
	}
	if status := fixture.ByLiveRegion("polite", "Switching workspace context…"); status == nil {
		t.Fatal("pending switch was not announced by the compiled component")
	}

	close(release)
	select {
	case got := <-committed:
		if got != target {
			t.Fatalf("committed selection = %+v, want %+v", got, target)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resolved replacement did not commit")
	}
	fixture.Stabilize()
	if scoped.projection != "tenant-b/principal-b/self-b" || len(scoped.history) != 0 || scoped.storage != "" {
		t.Fatalf("successful commit did not clear/adopt atomically: %+v", scoped)
	}
	if trigger := web038WASMSummary(fixture); trigger == nil || trigger.Text() != "Northwind · Northwind employee" {
		t.Fatalf("resolved replacement was not rendered: %+v", trigger)
	}
	if fixture.ByID("web038-shell-marker").NodeID() != stableMarkerID ||
		fixture.ByID("workspace-navigation").NodeID() != stableNavigationID ||
		fixture.ByID("main-content").NodeID() != stableMainID ||
		len(fixture.AllByTag("header")) != 1 || fixture.AllByTag("header")[0].NodeID() != stableBannerID {
		t.Fatal("context replacement remounted stable shell chrome")
	}
	for _, warning := range fixture.BuildWarningDiagnostics() {
		if strings.Contains(warning.Message, "TransientPopover") {
			continue // Existing shell siblings; outside WEB-038 ownership.
		}
		t.Fatalf("compiled context switch emitted a GWC diagnostic: %+v", warning)
	}
	fixture.Cleanup()

	t.Run("failure remains generic and preserves scoped state", func(t *testing.T) {
		failureFixture := renderfixture.New(t)
		failureScoped := &web038WASMScopedState{projection: "tenant-a/principal-a/self-a", history: []string{"people"}, storage: "ledger"}
		failureFixture.Render(ui.CreateElement(web038WASMStableShell, web038WASMShellProps{
			Initial: initial, Controller: &productui.ContextSwitchController{}, Adapter: &web038WASMStagedAdapter{}, Scoped: failureScoped,
			Exchange: func(context.Context, productui.ContextSelection) (productui.ContextSwitchResult, error) {
				return productui.ContextSwitchResult{}, errors.New("tenant-b bearer-super-secret")
			},
		}))
		failureFixture.ByRole("button", "Switch to Northwind, Northwind employee").Click()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			failureFixture.Stabilize()
			if failureFixture.ByLiveRegion("polite", productui.ResolveProductLocale("en-US").Text("context_switcher.failed")) != nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		if failureFixture.ByLiveRegion("polite", productui.ResolveProductLocale("en-US").Text("context_switcher.failed")) == nil {
			t.Fatal("failure was not announced with sanitized copy")
		}
		if failureScoped.projection != "tenant-a/principal-a/self-a" || len(failureScoped.history) != 1 || failureScoped.storage != "ledger" {
			t.Fatalf("failed exchange mutated scoped state: %+v", failureScoped)
		}
	})
}

type web038WASMScopedState struct {
	mu         sync.Mutex
	projection string
	history    []string
	storage    string
}

type web038WASMShellProps struct {
	Initial    productui.View
	Controller *productui.ContextSwitchController
	Exchange   productui.ContextExchange
	Adapter    *web038WASMStagedAdapter
	Scoped     *web038WASMScopedState
	Committed  chan<- productui.ContextSelection
}

func web038WASMStableShell(props web038WASMShellProps) ui.Node {
	marker := ui.UseState("stable")
	current := ui.UseState(props.Initial)
	view := current.Get()
	switcher := view.ContextSwitcher
	switcher.Controller = props.Controller
	switcher.Exchange = props.Exchange
	transaction := &web038WASMCommitSnapshot{}
	switcher.Commit = func(result productui.ContextSwitchResult) error {
		if props.Scoped == nil || props.Adapter == nil {
			return errors.New("missing scoped-state transaction")
		}
		next, ok := props.Adapter.take(result.ProjectionRef)
		if !ok || !web038WASMReceiptMatchesView(result, next) {
			return errors.New("missing or foreign staged projection")
		}
		props.Scoped.mu.Lock()
		defer props.Scoped.mu.Unlock()
		transaction.captured = true
		transaction.view = view
		transaction.projection = props.Scoped.projection
		transaction.history = append([]string(nil), props.Scoped.history...)
		transaction.storage = props.Scoped.storage
		props.Scoped.history = nil
		props.Scoped.storage = ""
		switch next.ContextSwitcher.Current.TenantID {
		case "tenant-a":
			props.Scoped.projection = "tenant-a/principal-a/" + next.ContextSwitcher.Current.ActingContextID
		case "tenant-b":
			props.Scoped.projection = "tenant-b/principal-b/" + next.ContextSwitcher.Current.ActingContextID
		default:
			return errors.New("foreign replacement")
		}
		current.Set(next)
		if props.Committed != nil {
			props.Committed <- result.Selection
		}
		return nil
	}
	switcher.Rollback = func() {
		if props.Scoped == nil || !transaction.captured {
			return
		}
		props.Scoped.mu.Lock()
		defer props.Scoped.mu.Unlock()
		props.Scoped.projection = transaction.projection
		props.Scoped.history = append([]string(nil), transaction.history...)
		props.Scoped.storage = transaction.storage
		current.Set(transaction.view)
	}
	view.ContextSwitcher = switcher
	return html.Div(html.Props{ID: "web038-shell-boundary"},
		html.Tag("output", html.Props{ID: "web038-shell-marker"}, ui.Text(marker.Get())),
		productui.BuildShell(view, html.Section(html.Props{ID: "web038-route-outlet"}, ui.Text("route")), true),
	)
}

type web038WASMCommitSnapshot struct {
	captured   bool
	view       productui.View
	projection string
	history    []string
	storage    string
}

// web038WASMStagedAdapter owns full replacement Views outside the feature
// component's props. ContextSwitchResult carries only a bounded receipt whose
// opaque reference selects a stage during the atomic commit transaction.
type web038WASMStagedAdapter struct {
	mu     sync.Mutex
	next   uint64
	staged map[string]productui.View
}

func (adapter *web038WASMStagedAdapter) stage(selection productui.ContextSelection, view productui.View) productui.ContextSwitchResult {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	adapter.next++
	ref := "projection-" + strconv.FormatUint(adapter.next, 10)
	if adapter.staged == nil {
		adapter.staged = make(map[string]productui.View)
	}
	adapter.staged[ref] = view
	return productui.ContextSwitchResult{
		Selection: selection, Current: view.ContextSwitcher.Current,
		Options: append([]productui.AuthorityContextOption(nil), view.ContextSwitcher.Options...),
		Page:    view.Page, TenantLabel: view.Tenant, PrincipalLabel: view.Principal,
		ProjectionRef: ref,
	}
}

func (adapter *web038WASMStagedAdapter) take(ref string) (productui.View, bool) {
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	view, ok := adapter.staged[ref]
	delete(adapter.staged, ref)
	return view, ok
}

func web038WASMReceiptMatchesView(receipt productui.ContextSwitchResult, view productui.View) bool {
	current := view.ContextSwitcher.Current
	return receipt.Selection == (productui.ContextSelection{TenantID: current.TenantID, ActingContextID: current.ActingContextID}) &&
		receipt.Current == current && slices.Equal(receipt.Options, view.ContextSwitcher.Options) &&
		receipt.Page == view.Page && receipt.TenantLabel == view.Tenant && receipt.PrincipalLabel == view.Principal
}

func web038WASMView(selection productui.ContextSelection) productui.View {
	options := []productui.AuthorityContextOption{
		{TenantID: "tenant-a", TenantName: "HarborCare", ActingContextID: "self-a", ActingContextName: "Your own authority"},
		{TenantID: "tenant-a", TenantName: "HarborCare", ActingContextID: "delegate-a", ActingContextName: "Covering HR", Delegated: true, Elevated: true, Delegator: "Maya Chen", ExpiresAt: "2026-09-18"},
		{TenantID: "tenant-b", TenantName: "Northwind", ActingContextID: "self-b", ActingContextName: "Northwind employee"},
	}
	current := productui.AuthorityContext{}
	for _, option := range options {
		if selection == (productui.ContextSelection{TenantID: option.TenantID, ActingContextID: option.ActingContextID}) {
			current = productui.AuthorityContext{TenantID: option.TenantID, TenantName: option.TenantName, ActingContextID: option.ActingContextID, ActingContextName: option.ActingContextName}
		}
	}
	if selection.TenantID == "tenant-b" {
		// The replacement deliberately changes the child count. Each option owns
		// its UseEvent hook behind a keyed component boundary, so this cannot
		// shift or reuse another pair's callback state.
		options = append([]productui.AuthorityContextOption(nil), options[2:]...)
	}
	view := productui.ApplyRoleVisibility(productui.NewView(productui.PageHome, current.TenantName, "Resolved principal", "work"), []string{"manager"})
	view.Viewer = productui.ViewerProfile{Name: "Resolved principal", Role: "Manager"}
	view.ContextSwitcher = productui.ContextSwitcherProps{
		I18nProps: productui.I18nProps{Locale: productui.ResolveProductLocale("en-US")},
		Current:   current, Options: options, State: productui.ContextSwitcherReady,
	}
	return view
}

func wantWEB038WASMSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal(message)
	}
}

func web038WASMSummary(fixture *renderfixture.Fixture) *renderfixture.QueryNode {
	for _, summary := range fixture.AllByTag("summary") {
		if summary.Attr("aria-label") == "Workspace context" {
			return summary
		}
	}
	return nil
}
