//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// The agent access pieces of the Agents area (AGENT2-018, AGENT2-019,
// AGENTCOST-006): the person's own access view, the administrator's connection
// console and the cost and spend-limit cards. Each is a placeholder the server
// renders; this file finds it, reads its data from the JSON routes, draws the
// page piece into it and handles its clicks and forms. One observer, one click,
// one change and one submit listener serve all of them, and none uses a timer.

const agentAccessCallTimeout = 30 * time.Second

var agentAccessBrowser struct {
	sync.Mutex
	cfg    journeyclient.Config
	bound  bool
	access agentAccessSurface
	admin  agentAdminSurface
	cost   agentCostSurface
	spend  agentSpendSurface
}

func configureAgentAccess(cfg journeyclient.Config) {
	agentAccessBrowser.Lock()
	changed := agentAccessBrowser.cfg.Bearer != cfg.Bearer
	agentAccessBrowser.cfg = journeyclientHTTPConfig(cfg)
	if changed {
		agentAccessBrowser.access = agentAccessSurface{}
		agentAccessBrowser.admin = agentAdminSurface{}
		agentAccessBrowser.cost = agentCostSurface{}
		agentAccessBrowser.spend = agentSpendSurface{}
	}
	bind := !agentAccessBrowser.bound
	agentAccessBrowser.bound = true
	agentAccessBrowser.Unlock()
	if bind {
		document := js.Global().Get("document")
		document.Call("addEventListener", "click", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				handleAgentAccessClick(args[0])
				handleAgentAdminClick(args[0])
				handleAgentCostClick(args[0])
				handleAgentSpendClick(args[0])
			}
			return nil
		}))
		document.Call("addEventListener", "submit", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				handleAgentAdminSubmit(args[0])
				handleAgentSpendSubmit(args[0])
			}
			return nil
		}))
		document.Call("addEventListener", "change", js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				handleAgentAdminChange(args[0])
			}
			return nil
		}))
		observe := js.FuncOf(func(_ js.Value, _ []js.Value) any { findAgentAccessMounts(); return nil })
		observer := js.Global().Get("MutationObserver").New(observe)
		observer.Call("observe", document.Get("body"), map[string]any{"childList": true, "subtree": true, "attributes": true, "attributeFilter": []any{"data-locale"}})
	}
	findAgentAccessMounts()
}

func journeyclientHTTPConfig(cfg journeyclient.Config) journeyclient.Config {
	return personaChatHTTPConfig(cfg)
}

func agentAccessConfig() journeyclient.Config {
	agentAccessBrowser.Lock()
	defer agentAccessBrowser.Unlock()
	return agentAccessBrowser.cfg
}

func agentMountLocale(mount js.Value) productui.LocaleContext {
	return productui.ResolveProductLocale(domAttribute(mount, "data-locale")).WithTimeZone(agentViewerTimeZone())
}

func renderAgentMarkup(mount js.Value, node ui.Node) {
	if !mount.Truthy() || !mount.Get("isConnected").Bool() {
		return
	}
	if markup, err := ui.RenderToString(node); err == nil {
		mount.Set("innerHTML", markup)
		mount.Call("setAttribute", "aria-busy", "false")
	}
}

func agentFormValue(form js.Value, name string) string {
	field := form.Call("querySelector", "[name='"+name+"']")
	if !field.Truthy() {
		return ""
	}
	return field.Get("value").String()
}

func agentFormChecked(form js.Value, name string) []string {
	var out []string
	boxes := form.Call("querySelectorAll", "[name='"+name+"']:checked")
	for i := 0; i < boxes.Length(); i++ {
		out = append(out, boxes.Index(i).Get("value").String())
	}
	return out
}

func agentClosest(event js.Value, selector string) js.Value {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return js.Undefined()
	}
	return target.Call("closest", selector)
}

// findAgentAccessMounts looks for every placeholder this file fills and starts
// the read of each one that is new.
func findAgentAccessMounts() {
	findAgentAccessMount()
	findAgentAdminMount()
	findAgentCostMount()
	findAgentSpendMounts()
}

// agentAccessNoop is the client the access pages are drawn with: the page's
// buttons are enabled when a client exists, and the script (not the client)
// does the work when they are pressed.
type agentAccessNoop struct{}

func (agentAccessNoop) StartProviderAuthorization(string) (productui.AgentAuthorizationStart, error) {
	return productui.AgentAuthorizationStart{}, nil
}
func (agentAccessNoop) UnlinkConnection(string) error             { return nil }
func (agentAccessNoop) RevokeDelegation(string) error             { return nil }
func (agentAccessNoop) SaveSpendLimit(string, int64, int64) error { return nil }
func (agentAccessNoop) CreateConnectionRevision(productui.AgentConnectionRevision) error {
	return nil
}
func (agentAccessNoop) ImportMCPSnapshot(string, string) error  { return nil }
func (agentAccessNoop) PublishConnectionRevision(string) error  { return nil }
func (agentAccessNoop) RequestSecondAdminApproval(string) error { return nil }
func (agentAccessNoop) ApproveRevision(string) error            { return nil }
func (agentAccessNoop) RollBack(string) error                   { return nil }

// ---- the person's own access view ----

type agentAccessSurface struct {
	mount    js.Value
	locale   string
	loaded   bool
	state    productui.AgentAccessLoadState
	snapshot productui.AgentAccessSnapshot
	message  string
	busy     bool
}

const agentsRoutePath = "/workspace/app/chat/agents"

func agentAccessViewActive() bool {
	location := js.Global().Get("location")
	return location.Get("pathname").String() == agentsRoutePath && productui.AgentAccessViewRequested(location.Get("search").String())
}

func findAgentAccessMount() {
	if !agentAccessViewActive() {
		agentAccessBrowser.Lock()
		agentAccessBrowser.access = agentAccessSurface{}
		agentAccessBrowser.Unlock()
		return
	}
	mount := js.Global().Get("document").Call("getElementById", "agent-access-mount")
	if !mount.Truthy() {
		return
	}
	locale := domAttribute(mount, "data-locale")
	agentAccessBrowser.Lock()
	surface := &agentAccessBrowser.access
	if surface.mount.Truthy() && surface.mount.Equal(mount) && surface.locale == locale {
		agentAccessBrowser.Unlock()
		return
	}
	*surface = agentAccessSurface{mount: mount, locale: locale, state: productui.AgentAccessStateLoading}
	agentAccessBrowser.Unlock()
	// The provider sends the person back to this page with the code and the
	// state it was given; finishing the link is the first thing to do then.
	query := js.Global().Get("URLSearchParams").New(js.Global().Get("location").Get("search").String())
	code, state := query.Call("get", "code"), query.Call("get", "state")
	if code.Truthy() && state.Truthy() {
		go completeAgentLink(mount, state.String(), code.String())
		return
	}
	go loadAgentAccess(mount, "")
}

func renderAgentAccess(mount js.Value) {
	agentAccessBrowser.Lock()
	surface := agentAccessBrowser.access
	agentAccessBrowser.Unlock()
	renderAgentMarkup(mount, productui.AgentAccessPage(productui.AgentAccessPageProps{
		I18nProps: productui.I18nProps{Locale: agentMountLocale(mount)}, State: surface.state, Snapshot: surface.snapshot, Client: agentAccessNoop{}, Message: surface.message, Embedded: true,
	}))
}

func loadAgentAccess(mount js.Value, message string) {
	cfg := agentAccessConfig()
	ctx, cancel := context.WithTimeout(context.Background(), agentAccessCallTimeout)
	defer cancel()
	var snapshot productui.AgentAccessSnapshot
	err := agentAccessCall(ctx, http.DefaultClient, cfg, http.MethodGet, agentAccessAPI+"/snapshot", nil, &snapshot)
	ui.PostAsync(func() {
		agentAccessBrowser.Lock()
		surface := &agentAccessBrowser.access
		if !surface.mount.Truthy() || !surface.mount.Equal(mount) {
			agentAccessBrowser.Unlock()
			return
		}
		surface.busy = false
		if err == nil {
			surface.state, surface.snapshot, surface.message, surface.loaded = productui.AgentAccessStateReady, snapshot, message, true
		} else if surface.loaded {
			// A read that fails keeps what the person last saw and says so.
			surface.message = "failed"
		} else {
			surface.state = productui.AgentAccessStateUnavailable
		}
		agentAccessBrowser.Unlock()
		renderAgentAccess(mount)
	})
}

func completeAgentLink(mount js.Value, state, code string) {
	cfg := agentAccessConfig()
	ctx, cancel := context.WithTimeout(context.Background(), agentAccessCallTimeout)
	defer cancel()
	var reply struct{ ConnectionID string }
	err := agentAccessCall(ctx, http.DefaultClient, cfg, http.MethodPost, agentAccessAPI+"/link/complete", agentAccessCompleteRequest{State: state, Code: code}, &reply)
	ui.PostAsync(func() {
		// The code and the state are spent whether or not the link worked: they
		// leave the address bar so a reload cannot offer them again.
		location := js.Global().Get("location")
		js.Global().Get("history").Call("replaceState", nil, "", location.Get("pathname").String()+"?view=access")
		message := "linked"
		if err != nil {
			message = agentAccessMessageKey(err)
		}
		go loadAgentAccess(mount, message)
	})
}

func handleAgentAccessClick(event js.Value) {
	button := agentClosest(event, "[data-agent-access-action]")
	if !button.Truthy() {
		return
	}
	agentAccessBrowser.Lock()
	mount := agentAccessBrowser.access.mount
	busy := agentAccessBrowser.access.busy
	agentAccessBrowser.Unlock()
	if !mount.Truthy() || !mount.Call("contains", button).Bool() || busy {
		return
	}
	action := domAttribute(button, "data-agent-access-action")
	connection, task := domAttribute(button, "data-connection-id"), domAttribute(button, "data-task-id")
	agentAccessBrowser.Lock()
	agentAccessBrowser.access.busy = true
	agentAccessBrowser.Unlock()
	button.Set("disabled", true)
	go runAgentAccessAction(mount, action, connection, task)
}

func runAgentAccessAction(mount js.Value, action, connection, task string) {
	cfg := agentAccessConfig()
	ctx, cancel := context.WithTimeout(context.Background(), agentAccessCallTimeout)
	defer cancel()
	finish := func(message string) {
		ui.PostAsync(func() { go loadAgentAccess(mount, message) })
	}
	switch action {
	case "retry":
		finish("")
	case "link":
		var start productui.AgentAuthorizationStart
		err := agentAccessCall(ctx, http.DefaultClient, cfg, http.MethodPost, agentAccessAPI+"/link/start", agentAccessLinkRequest{ConnectionID: connection}, &start)
		if err != nil || !start.ValidPKCE() {
			finish(agentAccessMessageKey(err))
			return
		}
		// Linking leaves this page for the provider's own sign-in and comes back
		// with a code; nothing on this page ever holds a provider credential.
		ui.PostAsync(func() { js.Global().Get("location").Set("href", start.AuthorizationURL) })
	case "unlink":
		if err := agentAccessCall(ctx, http.DefaultClient, cfg, http.MethodPost, agentAccessAPI+"/unlink", agentAccessLinkRequest{ConnectionID: connection}, nil); err != nil {
			finish(agentAccessMessageKey(err))
			return
		}
		finish("unlinked")
	case "revoke":
		if err := agentAccessCall(ctx, http.DefaultClient, cfg, http.MethodPost, agentAccessAPI+"/grants/revoke", agentAccessRevokeRequest{TaskID: task}, nil); err != nil {
			finish(agentAccessMessageKey(err))
			return
		}
		finish("revoked")
	default:
		finish("")
	}
}
