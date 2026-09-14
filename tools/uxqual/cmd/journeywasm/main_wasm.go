//go:build js && wasm

// Command journeywasm is the Promotion journey page's client:
// `go run ./tools/uxqual/cmd/journeywasm -out internal/humanwork/workspace/assets`
// builds this file into assets/journey.wasm, and
// internal/humanwork/workspace's journey shell loads it.
//
// Everything it does is in main below, and there is very little of it, which
// is the point: the configuration, the routing, the projection and the state
// machine are all in tools/uxqual/journeyclient, which has no syscall/js in
// it and is therefore tested by ordinary `go test`. What is left here is the
// four things only a browser can do -- read the island out of the document,
// dial the tunnel, mount the renderer, and follow the address bar.
package main

import (
	"context"
	"errors"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoGRPCBridge/pkg/wasm/dialer"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/taskmux"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// The shell's half of the contract (internal/humanwork/workspace's
// journey_shell.go). These are restated rather than imported because a
// tools/ command that imported the serving package would drag the whole
// server into a wasm binary; internal/humanwork/workspace's own test asserts
// the same two identifiers.
const (
	configElementID = "journey-config"
	rootElementID   = journey.RootElementID
	rootSelector    = journey.RootSelector
)

func main() {
	if err := start(); err != nil {
		mountStartupFailure()
	}
	// The page's work happens on the browser's event loop from here: every
	// RPC answer, every keystroke and every hash change arrives as a
	// callback. main must not return, or the Go runtime exits and takes all
	// of them with it.
	select {}
}

// start brings the client up. It returns an error only for the two failures
// that leave nothing working at all -- an unreadable island and a client
// that cannot be constructed -- because every later failure is an RPC
// refusal, which the page itself is the right place to show.
func start() error {
	raw, err := readIsland(configElementID)
	if err != nil {
		return err
	}
	cfg, err := journeyclient.ParseConfig([]byte(raw))
	if err != nil {
		return err
	}
	if root := js.Global().Get("document").Get("documentElement"); root.Truthy() {
		cfg.Locale = productui.ResolveProductLocale(root.Get("lang").String()).Resolved
	}
	conn, err := dial(cfg)
	if err != nil {
		return err
	}
	service := newJourneyService(conn, cfg)
	if isProductPath(currentPath()) {
		return startProduct(context.Background(), cfg, service)
	}

	store := journey.NewStore(journey.Page{})
	app := journeyclient.New(cfg, service, store, time.Now)
	// Standalone Journeys needs the same bounded finite-work lane as the
	// product shell. A long-lived watch remains on App.Async, outside it.
	app.Tasks = taskmux.New(taskmux.Options{MaxRunning: 4, MaxQueued: 64, PriorityBurst: 8})
	app.FocusField = focusJourneyField
	// Navigation goes through the address bar rather than straight into the
	// state machine, so the fragment and the page can never disagree and the
	// browser's own Back button works.
	app.Locate = setHash

	if err := mount(store); err != nil {
		return err
	}
	js.Global().Set("onhashchange", js.FuncOf(func(js.Value, []js.Value) any {
		app.OnHashChange(currentHash())
		return nil
	}))
	app.Start(context.Background(), currentHash())
	return nil
}

// newJourneyService keeps the WASM composition on the qualified adapter seam;
// the command owns only the browser dial and passes the connection to the
// generated-contract client boundary.
func newJourneyService(conn grpc.ClientConnInterface, cfg journeyclient.Config) journeyclient.Service {
	return journeyclient.NewGRPCService(conn, cfg.Bearer)
}

// dial builds the gRPC client over the browser's WebSocket.
//
// Three things here are deliberate. The dial target is a passthrough address
// (see Config.DialTarget) because grpc.NewClient's default resolver is DNS,
// which a browser cannot run. The dialer is handed the tunnel's own ws:// or
// wss:// URL, which is what it actually opens; the target is ignored by it.
// And the credentials are insecure at the gRPC layer on purpose: transport
// security is the browser's wss:// socket, which gRPC inside a WASM page
// cannot see and must not try to negotiate.
func dial(cfg journeyclient.Config) (*grpc.ClientConn, error) {
	return grpc.NewClient(
		cfg.DialTarget(),
		dialer.New(cfg.TunnelURL),
		// Browser mutations are never replayed by this composition. The
		// server owns idempotency; the client only reports the one attempt.
		grpc.WithDisableRetry(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

// mount clears the shell's no-script fallback and renders the live client in
// its place.
//
// The clearing is this command's job rather than the renderer's: GWC's
// reconciler mounts its tree into the container it is given and leaves any
// DOM already there alone, so without this the fallback paragraph would sit
// above a working page telling its reader the page does not work.
// textContent is used rather than innerHTML because it is not an injection
// sink, and clearing a node needs nothing that is.
func mount(store *journey.Store) error {
	if root := js.Global().Get("document").Call("getElementById", rootElementID); root.Truthy() {
		root.Set("textContent", "")
	}
	if err := journey.MountLive(store, rootSelector); err != nil {
		return err
	}
	bindActionableNoticeFocus(store)
	bindConfirmationDialogs(store)
	return nil
}

// bindActionableNoticeFocus brings a newly rendered refusal into view. The
// promotion form can be taller than the viewport, so leaving its summary at
// the top of the page makes a failed submit look like a dead button.
func bindActionableNoticeFocus(store *journey.Store) {
	lastNotice := ""
	var lastInvalidRevision uint64
	store.Subscribe(func() {
		page := store.Page()
		if page.FocusInvalidRevision != 0 {
			if page.FocusInvalidRevision != lastInvalidRevision {
				lastInvalidRevision = page.FocusInvalidRevision
				scheduleInvalidFieldFocus(store, page.FocusInvalidRevision, 0)
			}
			return
		}
		var callback js.Func
		callback = js.FuncOf(func(js.Value, []js.Value) any {
			defer callback.Release()
			document := js.Global().Get("document")
			notice := document.Call("querySelector", `.jn-notice[data-tone="warning"],.jn-notice[data-tone="danger"]`)
			if !notice.Truthy() {
				lastNotice = ""
				return nil
			}
			text := notice.Get("textContent").String()
			if text == "" || text == lastNotice {
				return nil
			}
			lastNotice = text
			target := document.Call("querySelector", `[aria-invalid="true"]`)
			if !target.Truthy() {
				target = notice
				target.Call("setAttribute", "tabindex", "-1")
			}
			target.Call("focus", map[string]any{"preventScroll": true})
			target.Call("scrollIntoView", map[string]any{"behavior": "smooth", "block": "center"})
			return nil
		})
		js.Global().Call("requestAnimationFrame", callback)
	})
}

// A store notification can precede GWC's DOM commit. Retry only for the
// bounded render window and only while the same rejected attempt is current;
// later typing or navigation must not steal focus back from the reader.
func scheduleInvalidFieldFocus(store *journey.Store, revision uint64, frame int) {
	if frame >= 8 {
		return
	}
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		defer callback.Release()
		if store.Page().FocusInvalidRevision != revision {
			return nil
		}
		target := js.Global().Get("document").Call("querySelector", `[aria-invalid="true"]`)
		if target.Truthy() {
			focusJourneyElement(target)
			return nil
		}
		scheduleInvalidFieldFocus(store, revision, frame+1)
		return nil
	})
	js.Global().Call("requestAnimationFrame", callback)
}

func focusJourneyField(fieldID string) {
	if fieldID == "" {
		return
	}
	if target := js.Global().Get("document").Call("getElementById", fieldID); target.Truthy() {
		focusJourneyElement(target)
	}
}

func focusJourneyElement(target js.Value) {
	target.Call("focus", map[string]any{"preventScroll": true})
	target.Call("scrollIntoView", map[string]any{"behavior": "auto", "block": "center"})
}

// errNoIsland is the one document-shaped failure: the shell always writes
// the island, so its absence means this bundle is loaded by something that
// is not the journey shell.
var errNoIsland = errors.New("journeywasm: the document carries no journey-config island")

// readIsland reads the JSON island's text.
func readIsland(id string) (string, error) {
	element := js.Global().Get("document").Call("getElementById", id)
	if !element.Truthy() {
		return "", errNoIsland
	}
	text := element.Get("textContent")
	if text.Type() != js.TypeString {
		return "", errNoIsland
	}
	return text.String(), nil
}

// currentHash is the address bar's fragment.
func currentHash() string {
	hash := js.Global().Get("location").Get("hash")
	if hash.Type() != js.TypeString {
		return ""
	}
	return hash.String()
}

// setHash changes the address bar, which comes back as a hashchange event.
func setHash(href string) {
	js.Global().Get("location").Set("hash", href)
}

// mountStartupFailure renders the one thing a client that cannot start can
// still say honestly.
//
// It goes through the same renderer as everything else, so the page a reader
// sees on failure is the page they know, with a safe recovery step in the
// notice rather than a raw connection or configuration error.
func mountStartupFailure() {
	locale := productui.ResolveProductLocale("")
	if document := js.Global().Get("document"); document.Type() == js.TypeObject {
		if root := document.Get("documentElement"); root.Type() == js.TypeObject {
			if lang := root.Get("lang"); lang.Type() == js.TypeString {
				locale = productui.ResolveProductLocale(lang.String())
			}
		}
	}
	_ = mount(journey.NewStore(startupFailurePage(locale)))
}

// startupFailurePage is the failure page as a value, so it can be asserted
// without a DOM.
func startupFailurePage(locale productui.LocaleContext) journey.Page {
	return journey.Page{
		Title: "Promotion journey · " + journeyclient.Brand,
		Brand: journeyclient.Brand,
		Nav: []journey.NavLink{
			{Label: "Workspace", Href: journeyclient.WorkspacePath},
		},
		Notice: &journey.Notice{
			Tone:   "danger",
			Title:  locale.Text("journey.startup_title"),
			Detail: locale.Text("journey.startup_detail"),
		},
		Footer: journey.Footer{
			Lines: []string{
				locale.Text("journey.startup_footer"),
			},
		},
	}
}
