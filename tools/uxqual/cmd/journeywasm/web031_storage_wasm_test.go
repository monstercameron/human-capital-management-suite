//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/router"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/productclient"
)

// TestTodo_WEB_031_Browser runs in the real Go js/wasm runtime under Node and
// invokes the production browser-storage adapter. The small Web Storage
// capability objects below exercise only the native JS API seam; no router,
// DOM, network or application state is mocked.
func TestTodo_WEB_031_Browser(t *testing.T) {
	if !js.Global().Truthy() {
		t.Fatal("js runtime did not expose global object")
	}
	ledgerID := newProductHistoryLedgerID()
	if !productclient.ValidHistoryLedgerID(ledgerID) {
		t.Fatalf("browser ledger id %q is not an exact opaque v1 identifier", ledgerID)
	}
	object := js.Global().Get("Object")
	storage := object.New()
	values := map[string]string{}
	setItem := js.FuncOf(func(_ js.Value, args []js.Value) any {
		values[args[0].String()] = args[1].String()
		return nil
	})
	getItem := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if value, ok := values[args[0].String()]; ok {
			return value
		}
		return nil
	})
	defer setItem.Release()
	defer getItem.Release()
	storage.Set("setItem", setItem)
	storage.Set("getItem", getItem)
	oldStorage := js.Global().Get("sessionStorage")
	js.Global().Set("sessionStorage", storage)
	defer js.Global().Set("sessionStorage", oldStorage)

	controller := &browserProductHistoryController{id: ledgerID, maxIndex: 31}
	controller.writeMax()
	key := productclient.HistoryStorageKey(controller.id)
	if values[key] != "31" {
		t.Fatalf("production adapter stored %q = %q, want 31", key, values[key])
	}
	if got := readProductHistoryMax(controller.id); got != 31 {
		t.Fatalf("production adapter read watermark = %d, want 31", got)
	}
	visitKey := productclient.ChatVisitsStorageKey(strings.Repeat("a", 64))
	visitValue, valid := productclient.EncodeChatVisits([]productclient.ChatVisit{{ConversationID: strings.Repeat("b", 64), Count: 4}})
	if !valid || !browserStorageSet(storage, visitKey, visitValue) {
		t.Fatal("production adapter rejected a bounded chat visit presentation record")
	}
	if got, ok := browserStorageGet(storage, visitKey); !ok || got != visitValue {
		t.Fatalf("production adapter chat visit read = %q/%v, want the written record", got, ok)
	}
	if browserStorageSet(storage, "hcm-next.workflow.truth", "1") {
		t.Fatal("production adapter accepted an authority-shaped key")
	}
	if _, present := values["hcm-next.workflow.truth"]; present {
		t.Fatal("authority-shaped key reached the browser storage API")
	}

	// Web Storage exceptions are ordinary browser conditions (private mode,
	// quota and sandbox policy). They must be absorbed by the optional hint.
	throwing := js.Global().Call("eval", `({setItem(){throw new Error("quota")},getItem(){throw new Error("denied")}})`)
	js.Global().Set("sessionStorage", throwing)
	if browserStorageSet(throwing, key, "31") {
		t.Fatal("quota exception was reported as a successful write")
	}
	if value, ok := browserStorageGet(throwing, key); ok || value != "" {
		t.Fatalf("storage exception read = %q/%v, want empty/false", value, ok)
	}
	if got := readProductHistoryMax(controller.id); got != 0 {
		t.Fatalf("exceptional storage produced watermark %d, want fail-closed zero", got)
	}

	// A host with storage disabled exposes no object at all. This is the
	// non-throwing form of the same browser policy and must remain optional.
	js.Global().Call("eval", `delete globalThis.sessionStorage`)
	if _, ok := browserSessionStorage(); ok {
		t.Fatal("missing sessionStorage was reported as available")
	}
}

func TestTodo_WEB_031_Golden(t *testing.T) {
	// replaceState receives a closed object. A hostile old entry cannot smuggle
	// credentials, tenant, workflow or transaction-shaped fields into the next
	// entry merely because the router is refreshing its ledger annotation.
	object := js.Global().Get("Object")
	history := object.New()
	current := object.New()
	current.Set("tenant", "tenant-a")
	current.Set("credential", "Bearer secret")
	current.Set("workflow", "approved")
	history.Set("state", current)
	var captured js.Value
	replace := js.FuncOf(func(_ js.Value, args []js.Value) any {
		captured = args[0]
		return nil
	})
	defer replace.Release()
	history.Set("replaceState", replace)
	oldHistory := js.Global().Get("history")
	oldLocation := js.Global().Get("location")
	location := object.New()
	location.Set("href", "/workspace/app/home")
	js.Global().Set("history", history)
	js.Global().Set("location", location)
	defer js.Global().Set("history", oldHistory)
	defer js.Global().Set("location", oldLocation)

	controller := &browserProductHistoryController{id: "0123456789abcdef0123456789abcdef"}
	controller.replaceCurrentState(3)
	if captured.Type() != js.TypeObject {
		t.Fatal("replaceState did not receive a state object")
	}
	for _, field := range []string{"tenant", "credential", "workflow", "transaction", "authority"} {
		if captured.Get(field).Type() != js.TypeUndefined {
			t.Errorf("hostile history field %q survived state replacement", field)
		}
	}
	if captured.Get(productHistoryIDField).String() != controller.id || captured.Get(productHistoryIndexField).Int() != 3 {
		t.Fatalf("closed history state = %s/%d", captured.Get(productHistoryIDField).String(), captured.Get(productHistoryIndexField).Int())
	}
}

func TestTodo_WEB_031_Fault(t *testing.T) {
	t.Run("global storage getter throws", func(t *testing.T) {
		installThrowingGlobalGetter(t, "sessionStorage")
		if _, ok := browserSessionStorage(); ok {
			t.Fatal("throwing sessionStorage getter was reported as available")
		}
	})

	t.Run("global history getter throws", func(t *testing.T) {
		installThrowingGlobalGetter(t, "history")
		if _, ok := browserHistory(); ok {
			t.Fatal("throwing history getter was reported as available")
		}
		controller := newBrowserProductHistoryController()
		if controller.id != "" {
			t.Fatalf("controller minted ledger %q without an available History API", controller.id)
		}
	})

	t.Run("history accessors and methods throw", func(t *testing.T) {
		object := js.Global().Get("Object")
		oldHistory := js.Global().Get("history")
		oldLocation := js.Global().Get("location")
		t.Cleanup(func() {
			js.Global().Set("history", oldHistory)
			js.Global().Set("location", oldLocation)
		})

		throwingState := js.Global().Call("eval", `new Proxy({}, {get(){throw new Error("state denied")}})`)
		if id, index, ok := productHistoryState(throwingState); ok || id != "" || index != 0 {
			t.Fatalf("throwing history.state decoded as %q/%d/%v", id, index, ok)
		}

		throwingHistory := js.Global().Call("eval", `new Proxy({}, {get(_target, property){throw new Error(String(property)+" denied")}})`)
		js.Global().Set("history", throwingHistory)
		controller := &browserProductHistoryController{
			id: "0123456789abcdef0123456789abcdef", index: 1, maxIndex: 2,
		}
		if props := controller.Props(productui.ResolveProductLocale("en-US")); props.CanGoBack || props.CanGoForward || props.GoBack != nil || props.GoForward != nil {
			t.Fatalf("throwing history.state left controls enabled: %+v", props)
		}
		controller.Go(1) // must fail soft before invoking a throwing method
		if controller.replaceCurrentState(1) {
			t.Fatal("throwing replaceState was reported as successful")
		}

		state := object.New()
		state.Set(productHistoryIDField, controller.id)
		state.Set(productHistoryIndexField, 1)
		methodThrowingHistory := js.Global().Call("eval", `({replaceState(){throw new Error("replace denied")},go(){throw new Error("go denied")}})`)
		methodThrowingHistory.Set("state", state)
		js.Global().Set("history", methodThrowingHistory)
		location := js.Global().Call("eval", `new Proxy({}, {get(){throw new Error("location denied")}})`)
		js.Global().Set("location", location)
		if href := browserLocationHref(); href != "" {
			t.Fatalf("throwing location getter returned %q", href)
		}
		if path, query := currentPath(), currentQuery(); path != "" || query != "" {
			t.Fatalf("throwing location getter returned route %q?%q", path, query)
		}
		if controller.replaceCurrentState(1) {
			t.Fatal("throwing replaceState method was reported as successful")
		}
		if browserHistoryGo(methodThrowingHistory, 1) {
			t.Fatal("throwing history.go method was reported as successful")
		}
		controller.Go(1) // validated state followed by throwing go must still fail soft
	})
}

// TestTodo_WEB_031_GWCCompatibility uses GWC v5's real HistoryRouter. GWC
// intentionally pushes nil state; the product adapter then writes a closed,
// two-field marker. That closed state must remain compatible with GWC's own
// push, popstate and route rendering rather than preserving arbitrary state.
func TestTodo_WEB_031_GWCCompatibility(t *testing.T) {
	browser := installWASMHistory(t, productui.Path(productui.PageHome))
	productRouter := router.NewHistoryRouter(router.RouterOptions{DefaultRoute: productui.Path(productui.PageHome)})
	productRouter.SetFocusManagement(false)
	productRouter.Register(productui.Path(productui.PageHome), func(router.Attrs) *router.Element {
		return html.Div(html.Props{ID: "home"}, ui.Text("home"))
	})
	productRouter.Register(productui.Path(productui.PagePeople), func(router.Attrs) *router.Element {
		return html.Div(html.Props{ID: "people"}, ui.Text("people"))
	})

	oldProductHistory := productHistory
	productHistory = newBrowserProductHistoryController()
	t.Cleanup(func() { productHistory = oldProductHistory })
	if productRouter.Current() == nil {
		t.Fatal("GWC did not render the initial route with closed history state")
	}
	productHistory.Navigate(productRouter.Navigate, productui.Path(productui.PagePeople))
	if productRouter.Current() == nil || browser.path() != productui.Path(productui.PagePeople) {
		t.Fatalf("GWC push with closed history state failed: path=%q", browser.path())
	}
	assertClosedProductHistoryState(t, browser.entries[browser.index].state, productHistory.id, 1)

	props := productHistory.Props(productui.ResolveProductLocale("en-US"))
	if !props.CanGoBack || props.GoBack == nil {
		t.Fatalf("closed state did not expose bounded back navigation: %+v", props)
	}
	props.GoBack()
	if productRouter.Current() == nil || browser.path() != productui.Path(productui.PageHome) {
		t.Fatalf("GWC popstate with closed history state failed: path=%q", browser.path())
	}
	assertClosedProductHistoryState(t, browser.entries[browser.index].state, productHistory.id, 0)
}

func assertClosedProductHistoryState(t *testing.T, state js.Value, wantID string, wantIndex int) {
	t.Helper()
	if state.Type() != js.TypeObject {
		t.Fatalf("history state type = %s, want object", state.Type())
	}
	keys := js.Global().Get("Object").Call("keys", state)
	if keys.Length() != 2 {
		t.Fatalf("closed history state has %d enumerable fields, want 2", keys.Length())
	}
	if id, index, ok := productHistoryState(state); !ok || id != wantID || index != wantIndex {
		t.Fatalf("closed history state = %q/%d/%v, want %q/%d/true", id, index, ok, wantID, wantIndex)
	}
}

func installThrowingGlobalGetter(t *testing.T, name string) {
	t.Helper()
	factory := js.Global().Call("eval", `(function(name){
		const had = Object.prototype.hasOwnProperty.call(globalThis, name);
		const previous = Object.getOwnPropertyDescriptor(globalThis, name);
		Object.defineProperty(globalThis, name, {configurable:true, get(){throw new Error(name+" denied")}});
		return function(){if(had){Object.defineProperty(globalThis, name, previous)}else{delete globalThis[name]}};
	})`)
	restore := factory.Invoke(name)
	t.Cleanup(func() { restore.Invoke() })
}
