//go:build js && wasm

package main

import (
	"crypto/rand"
	"encoding/hex"
	"math"
	"net/url"
	"strings"
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
)

const (
	productHistoryIDField    = "hcmProductHistoryID"
	productHistoryIndexField = "hcmProductHistoryIndex"
)

var productHistory *browserProductHistoryController
var productHistoryControlsRefresh func()

func refreshProductHistoryControls() {
	if productHistoryControlsRefresh != nil {
		productHistoryControlsRefresh()
	}
}

// browserProductHistoryController annotates entries created by the software
// router. The standard History API can move in both directions but cannot
// report forward availability, so the controller retains the high-water mark
// in session storage. A ledger ID in history.state keeps stale sessions apart.
type browserProductHistoryController struct {
	id                        string
	index                     int
	maxIndex                  int
	pendingSoftwareNavigation string
}

func newBrowserProductHistoryController() *browserProductHistoryController {
	controller := &browserProductHistoryController{}
	history, historyOK := browserHistory()
	if !historyOK {
		return controller
	}
	state, stateOK := browserHistoryState(history)
	if id, index, ok := productHistoryState(state); stateOK && ok {
		controller.id = id
		controller.index = index
		controller.maxIndex = maxInt(index, readProductHistoryMax(id))
		return controller
	}
	if controller.id = newProductHistoryLedgerID(); controller.id == "" {
		// A browser without a secure random source cannot safely mint the
		// ledger namespace. Keep the router usable; only forward controls that
		// require this non-authoritative hint are disabled.
		return controller
	}
	controller.replaceCurrentState(0)
	controller.writeMax()
	return controller
}

func newProductHistoryLedgerID() string {
	var randomID [productclient.HistoryLedgerRandomBytes]byte
	if _, err := rand.Read(randomID[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(randomID[:])
}

// Navigate delegates to the production router, then annotates the entry it
// synchronously pushed. A new push discards the browser's forward branch.
func (controller *browserProductHistoryController) Navigate(navigate func(string), href string) {
	if navigate == nil {
		return
	}
	if controller != nil {
		controller.pendingSoftwareNavigation = normalizedProductHistoryHref(href)
	}
	navigate(href)
	if controller == nil || controller.id == "" {
		return
	}
	if history, historyOK := browserHistory(); historyOK {
		if state, stateOK := browserHistoryState(history); stateOK {
			if id, _, ok := productHistoryState(state); ok && id == controller.id {
				return
			}
		}
	}
	controller.index++
	controller.maxIndex = controller.index
	controller.replaceCurrentState(controller.index)
	controller.writeMax()
}

// RecordSameRoutePush advances the app-level history controls after a
// same-URL interaction adds a browser entry, such as selecting a chat room.
// The router's route does not change, so Navigate cannot observe this push.
func (controller *browserProductHistoryController) RecordSameRoutePush() {
	if controller == nil || controller.id == "" {
		return
	}
	history, historyOK := browserHistory()
	if !historyOK {
		return
	}
	state, stateOK := browserHistoryState(history)
	id, index, valid := productHistoryState(state)
	if !stateOK || !valid || id != controller.id || index >= productclient.MaxHistoryIndex {
		return
	}
	clone, cloneOK := cloneBrowserHistoryState(state)
	if !cloneOK {
		return
	}
	index++
	clone.Set(productHistoryIDField, controller.id)
	clone.Set(productHistoryIndexField, index)
	if !browserHistoryReplaceState(history, clone, browserLocationHref()) {
		return
	}
	controller.index = index
	controller.maxIndex = index
	controller.writeMax()
	refreshProductHistoryControls()
}

func cloneBrowserHistoryState(state js.Value) (clone js.Value, ok bool) {
	defer func() {
		if recover() != nil {
			clone = js.Undefined()
			ok = false
		}
	}()
	if state.Type() != js.TypeObject && state.Type() != js.TypeNull {
		return js.Undefined(), false
	}
	return js.Global().Get("Object").Call("assign", js.Global().Get("Object").New(), state), true
}

// ClaimSoftwareNavigation distinguishes a user-initiated software push from
// cold rendering and browser popstate resumption. Route loaders may persist
// presentation preferences only for the former; a history read must never
// replay a preference or workflow-use mutation.
func (controller *browserProductHistoryController) ClaimSoftwareNavigation(path, encodedQuery string) bool {
	if controller == nil {
		return false
	}
	pending := controller.pendingSoftwareNavigation
	controller.pendingSoftwareNavigation = ""
	current := path
	if encodedQuery != "" {
		current += "?" + encodedQuery
	}
	return pending != "" && pending == current
}

func normalizedProductHistoryHref(href string) string {
	parsed, err := url.Parse(strings.TrimSpace(href))
	if err != nil || parsed.Path == "" {
		return ""
	}
	normalized := parsed.Path
	if query := parsed.Query().Encode(); query != "" {
		normalized += "?" + query
	}
	return normalized
}

func (controller *browserProductHistoryController) Props(locale productui.LocaleContext) productui.HistoryNavigationProps {
	props := productui.HistoryNavigationProps{I18nProps: productui.I18nProps{Locale: locale}}
	if controller == nil || controller.id == "" {
		return props
	}
	history, historyOK := browserHistory()
	if !historyOK {
		return props
	}
	state, stateOK := browserHistoryState(history)
	id, index, stateOK := productHistoryState(state)
	if !stateOK || id != controller.id {
		return props
	}
	controller.index = index
	props.CanGoBack = controller.index > 0
	props.CanGoForward = controller.index < controller.maxIndex
	props.GoBack = func() { controller.Go(-1) }
	props.GoForward = func() { controller.Go(1) }
	return props
}

func (controller *browserProductHistoryController) Go(offset int) {
	if controller == nil || (offset != -1 && offset != 1) {
		return
	}
	history, historyOK := browserHistory()
	if !historyOK {
		return
	}
	state, stateOK := browserHistoryState(history)
	id, index, stateOK := productHistoryState(state)
	if !stateOK || id != controller.id {
		return
	}
	controller.index = index
	if offset < 0 && controller.index <= 0 || offset > 0 && controller.index >= controller.maxIndex {
		return
	}
	browserHistoryGo(history, offset)
}

func (controller *browserProductHistoryController) replaceCurrentState(index int) (ok bool) {
	history, historyOK := browserHistory()
	if !historyOK {
		return false
	}
	state := js.Global().Get("Object").New()
	state.Set(productHistoryIDField, controller.id)
	state.Set(productHistoryIndexField, index)
	return browserHistoryReplaceState(history, state, browserLocationHref())
}

func (controller *browserProductHistoryController) writeMax() {
	key := productclient.HistoryStorageKey(controller.id)
	value, ok := productclient.EncodeHistoryIndex(controller.maxIndex)
	storage, storageOK := browserSessionStorage()
	if storageOK && ok && productclient.ValidateBrowserStorageWrite(key, value) == nil {
		browserStorageSet(storage, key, value)
	}
}

func readProductHistoryMax(id string) int {
	key := productclient.HistoryStorageKey(id)
	if key == "" {
		return 0
	}
	storage, storageOK := browserSessionStorage()
	if !storageOK {
		return 0
	}
	raw, ok := browserStorageGet(storage, key)
	if !ok {
		return 0
	}
	value, ok := productclient.DecodeHistoryIndex(raw)
	if !ok {
		return 0
	}
	return value
}

func productHistoryState(state js.Value) (id string, index int, ok bool) {
	defer func() {
		if recover() != nil {
			id = ""
			index = 0
			ok = false
		}
	}()
	if state.Type() != js.TypeObject {
		return "", 0, false
	}
	idValue, idOK := browserProperty(state, productHistoryIDField)
	indexValue, indexOK := browserProperty(state, productHistoryIndexField)
	if !idOK || !indexOK {
		return "", 0, false
	}
	if idValue.Type() != js.TypeString || indexValue.Type() != js.TypeNumber {
		return "", 0, false
	}
	id = strings.TrimSpace(idValue.String())
	indexFloat := indexValue.Float()
	// history.state is browser-owned input. Do not let NaN, infinity,
	// fractions, or an unbounded value turn the history controls into a
	// surprising history.go request (or a platform-dependent int overflow).
	if !safeProductHistoryID(id) || math.IsNaN(indexFloat) || math.IsInf(indexFloat, 0) || indexFloat < 0 || math.Trunc(indexFloat) != indexFloat || indexFloat > float64(productclient.MaxHistoryIndex) {
		return "", 0, false
	}
	return id, int(indexFloat), true
}

func safeProductHistoryID(id string) bool {
	return productclient.ValidHistoryLedgerID(id)
}

// syscall/js.Value.Get lets a JavaScript getter exception escape past Go's
// panic/recover boundary. Reflect.get invoked through Value.Call reports the
// exception as a recoverable js.Error instead, so every untrusted browser or
// history-state property read must pass through this seam.
func browserProperty(target js.Value, name string) (value js.Value, ok bool) {
	defer func() {
		if recover() != nil {
			value = js.Undefined()
			ok = false
		}
	}()
	if target.Type() != js.TypeObject && target.Type() != js.TypeFunction {
		return js.Undefined(), false
	}
	reflect := js.Global().Get("Reflect")
	if reflect.Type() != js.TypeObject {
		return js.Undefined(), false
	}
	return reflect.Call("get", target, name), true
}

// Value.Call also performs its initial method lookup through Value.Get. Fetch
// the method with browserProperty and invoke it through trusted Reflect.apply so
// accessor and method exceptions both remain recoverable inside Go.
func browserCall(target js.Value, name string, arguments ...any) (value js.Value, ok bool) {
	defer func() {
		if recover() != nil {
			value = js.Undefined()
			ok = false
		}
	}()
	method, ok := browserProperty(target, name)
	if !ok || method.Type() != js.TypeFunction {
		return js.Undefined(), false
	}
	reflect := js.Global().Get("Reflect")
	if reflect.Type() != js.TypeObject {
		return js.Undefined(), false
	}
	return reflect.Call("apply", method, target, arguments), true
}

// The History API is an optional enhancement too. Browser policy, a sandbox or
// a hostile accessor can make property reads and methods throw. These wrappers
// keep the SSR document and GWC event loop usable while disabling only the
// affected history control.
func browserHistory() (history js.Value, ok bool) {
	defer func() {
		if recover() != nil {
			history = js.Undefined()
			ok = false
		}
	}()
	history, ok = browserProperty(js.Global(), "history")
	if !ok {
		return js.Undefined(), false
	}
	if history.Type() != js.TypeObject {
		return js.Undefined(), false
	}
	return history, true
}

func browserHistoryState(history js.Value) (state js.Value, ok bool) {
	defer func() {
		if recover() != nil {
			state = js.Undefined()
			ok = false
		}
	}()
	if history.Type() != js.TypeObject {
		return js.Undefined(), false
	}
	return browserProperty(history, "state")
}

func browserHistoryReplaceState(history, state js.Value, href string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	if history.Type() != js.TypeObject || state.Type() != js.TypeObject {
		return false
	}
	_, ok = browserCall(history, "replaceState", state, "", href)
	return ok
}

func browserHistoryGo(history js.Value, offset int) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	if history.Type() != js.TypeObject || (offset != -1 && offset != 1) {
		return false
	}
	_, ok = browserCall(history, "go", offset)
	return ok
}

func browserLocationHref() (href string) {
	defer func() {
		if recover() != nil {
			href = ""
		}
	}()
	location, ok := browserProperty(js.Global(), "location")
	if !ok || location.Type() != js.TypeObject {
		return ""
	}
	value, ok := browserProperty(location, "href")
	if !ok || value.Type() != js.TypeString {
		return ""
	}
	return value.String()
}

// Web Storage is an optional, user-controlled capability. Private browsing,
// disabled cookies, quota exhaustion and sandboxed iframes can make either
// operation throw a DOMException. A storage failure must never tear down the
// Go event loop or turn an optional hint into a hard navigation failure.
func browserStorageSet(storage js.Value, key, value string) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	if storage.Type() != js.TypeObject || key == "" || productclient.ValidateBrowserStorageWrite(key, value) != nil {
		return false
	}
	_, ok = browserCall(storage, "setItem", key, value)
	return ok
}

func browserSessionStorage() (storage js.Value, ok bool) {
	defer func() {
		if recover() != nil {
			storage = js.Undefined()
			ok = false
		}
	}()
	storage, ok = browserProperty(js.Global(), "sessionStorage")
	if !ok {
		return js.Undefined(), false
	}
	if storage.Type() != js.TypeObject {
		return js.Undefined(), false
	}
	return storage, true
}

func browserStorageGet(storage js.Value, key string) (value string, ok bool) {
	defer func() {
		if recover() != nil {
			value = ""
			ok = false
		}
	}()
	if storage.Type() != js.TypeObject || key == "" || productclient.ValidateHistoryStorageEntry(key, "0") != nil {
		return "", false
	}
	raw, callOK := browserCall(storage, "getItem", key)
	if !callOK || raw.Type() != js.TypeString {
		return "", false
	}
	return raw.String(), true
}

func applyBrowserHistoryNavigation(view *productui.View) {
	if view != nil && productHistory != nil {
		view.HistoryNavigation = productHistory.Props(view.Locale)
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
