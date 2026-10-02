//go:build js && wasm

package chatui

import (
	"strconv"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The conversation's menu is an anchored layer like every other (AGENTUX-062):
// syncChatAnchoredLayers places it against the row's button, so there is no
// separate placement of its own here.

func menuTriggerIsFocusVisible(event ui.Event) bool {
	target := event.JSValue().Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return false
	}
	trigger := target.Call("closest", `[data-action="rail-menu"],[data-action="menu"]`)
	return trigger.Truthy() && trigger.Get("matches").Type() == js.TypeFunction && trigger.Call("matches", ":focus-visible").Bool()
}

func focusRailMenu(dismiss func(), focusFirst bool) {
	var callback js.Func
	attempts := 0
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		menu := js.Global().Get("document").Call("querySelector", ".rail-row-menu [role=menuitem],.rail-row-menu [role=menuitemradio]")
		if menu.Truthy() {
			popup := menu.Call("closest", ".rail-row-menu")
			if popup.Truthy() {
				height := popup.Call("getBoundingClientRect").Get("height").Float()
				viewport := js.Global().Get("innerHeight").Float()
				top := popup.Call("getBoundingClientRect").Get("top").Float()
				if top+height > viewport-8 {
					popup.Get("style").Set("top", strconv.FormatFloat(max(8, viewport-height-8), 'f', 0, 64)+"px")
				}
			}
			if focusFirst {
				menu.Call("focus")
			}
			watchRailMenuDismiss(dismiss)
			callback.Release()
			return nil
		}
		attempts++
		if attempts < 15 {
			js.Global().Call("requestAnimationFrame", callback)
		} else {
			callback.Release()
		}
		return nil
	})
	js.Global().Call("requestAnimationFrame", callback)
}

func watchRailMenuDismiss(dismiss func()) {
	doc := js.Global().Get("document")
	if old := doc.Get("__chatRailMenuCleanup"); old.Truthy() {
		old.Invoke()
	}
	window := js.Global().Get("window")
	var listener, cleanup js.Func
	cleanup = js.FuncOf(func(js.Value, []js.Value) any {
		doc.Call("removeEventListener", "click", listener)
		doc.Call("removeEventListener", "scroll", listener, true)
		window.Call("removeEventListener", "resize", listener)
		doc.Set("__chatRailMenuCleanup", js.Undefined())
		listener.Release()
		cleanup.Release()
		return nil
	})
	listener = js.FuncOf(func(_ js.Value, args []js.Value) any {
		event := args[0]
		if event.Get("type").String() == "scroll" {
			// CHATBUG-090: only a scroll of the sidebar or the page closes the menu.
			if !chatbug090ScrollTargetDismisses(event.Get("target")) {
				return nil
			}
		}
		outside := event.Get("type").String() != "click"
		if !outside {
			target := event.Get("target")
			outside = !target.Truthy() || !target.Call("closest", ".chat-workspace").Truthy()
		}
		cleanup.Invoke()
		if outside {
			dismiss()
		}
		return nil
	})
	doc.Call("addEventListener", "click", listener)
	doc.Call("addEventListener", "scroll", listener, true)
	window.Call("addEventListener", "resize", listener)
	doc.Set("__chatRailMenuCleanup", cleanup)
}

func clearRailMenuDismiss() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	if cleanup := doc.Get("__chatRailMenuCleanup"); cleanup.Truthy() {
		cleanup.Invoke()
	}
}

func restoreRailMenuFocus(id string) {
	if !js.Global().Get("document").Truthy() || js.Global().Get("setTimeout").Type() != js.TypeFunction {
		return
	}
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		rows := js.Global().Get("document").Call("querySelectorAll", ".rail-row-more")
		for i := 0; i < rows.Get("length").Int(); i++ {
			button := rows.Index(i)
			if button.Get("dataset").Get("id").String() == id {
				button.Call("focus")
				break
			}
		}
		callback.Release()
		return nil
	})
	js.Global().Call("setTimeout", callback, 0)
}

func moveRailMenuFocus(event ui.Event) bool {
	target := event.JSValue().Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return false
	}
	inMenu := target.Call("closest", ".rail-row-menu").Truthy()
	trigger := target.Call("closest", `[data-action="rail-menu"]`)
	if !inMenu && !trigger.Truthy() {
		return false
	}
	key := event.JSValue().Get("key").String()
	if key != "ArrowDown" && key != "ArrowUp" && key != "Home" && key != "End" {
		return false
	}
	items := js.Global().Get("document").Call("querySelectorAll", ".rail-row-menu [role=menuitem]:not([disabled]),.rail-row-menu [role=menuitemradio]:not([disabled])")
	length := items.Get("length").Int()
	if length == 0 {
		return false
	}
	index := -1
	for i := 0; i < length; i++ {
		if items.Index(i).Equal(target) {
			index = i
			break
		}
	}
	switch key {
	case "ArrowDown":
		index = (index + 1) % length
	case "ArrowUp":
		if index < 0 {
			index = length - 1
		} else {
			index = (index + length - 1) % length
		}
	case "Home":
		index = 0
	case "End":
		index = length - 1
	}
	items.Index(index).Call("focus")
	return true
}

const messageMenuItems = "[role=menuitem]:not([disabled]),[role=menuitemradio]:not([disabled])"

var messageMenuTrigger js.Value
var messageMenuTriggerID string
var messageMenuGeometryID string
var messageMenuGeometryListener js.Func
var messageMenuGeometryObserver js.Value
var messageMenuGeometryNode js.Value

func rememberMessageMenuTrigger(event ui.Event, id string) {
	target := event.JSValue().Get("target")
	if target.Truthy() && target.Get("closest").Type() == js.TypeFunction {
		messageMenuTrigger = target.Call("closest", ".message-action[data-action=menu]")
		messageMenuTriggerID = id
	}
}

func syncOpenMessageMenu(id string) {
	if id == "" {
		clearMessageMenuGeometry()
		return
	}
	frame := js.Global().Get("requestAnimationFrame")
	if frame.Type() != js.TypeFunction {
		return
	}
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		if menu := messageMenuPopup(id); menu.Truthy() {
			positionMessageMenu(menu)
			watchMessageMenuGeometry(id, menu)
		}
		callback.Release()
		return nil
	})
	frame.Invoke(callback)
}

func watchMessageMenuGeometry(id string, menu js.Value) {
	if messageMenuGeometryID == id && messageMenuGeometryListener.Truthy() && messageMenuGeometryNode.Truthy() && messageMenuGeometryNode.Equal(menu) {
		return
	}
	clearMessageMenuGeometry()
	messageMenuGeometryID = id
	messageMenuGeometryNode = menu
	messageMenuGeometryListener = js.FuncOf(func(js.Value, []js.Value) any {
		if current := messageMenuPopup(id); current.Truthy() {
			positionMessageMenu(current)
		}
		return nil
	})
	window := js.Global()
	window.Call("addEventListener", "resize", messageMenuGeometryListener)
	window.Call("addEventListener", "scroll", messageMenuGeometryListener, true)
	observerClass := window.Get("ResizeObserver")
	if observerClass.Type() == js.TypeFunction {
		messageMenuGeometryObserver = observerClass.New(messageMenuGeometryListener)
		messageMenuGeometryObserver.Call("observe", menu)
		if bound := menu.Call("closest", ".message-list,.thread-scroll"); bound.Truthy() {
			messageMenuGeometryObserver.Call("observe", bound)
		}
	}
}

func clearMessageMenuGeometry() {
	if messageMenuGeometryListener.Truthy() {
		window := js.Global()
		window.Call("removeEventListener", "resize", messageMenuGeometryListener)
		window.Call("removeEventListener", "scroll", messageMenuGeometryListener, true)
		messageMenuGeometryListener.Release()
		messageMenuGeometryListener = js.Func{}
	}
	if messageMenuGeometryObserver.Truthy() {
		messageMenuGeometryObserver.Call("disconnect")
		messageMenuGeometryObserver = js.Undefined()
	}
	messageMenuGeometryID = ""
	messageMenuGeometryNode = js.Undefined()
}

func messageMenuPopup(id string) js.Value {
	doc := js.Global().Get("document")
	if !doc.Truthy() || doc.Get("querySelectorAll").Type() != js.TypeFunction {
		return js.Null()
	}
	menus := doc.Call("querySelectorAll", ".message-menu[data-message-menu]")
	for i := 0; i < menus.Get("length").Int(); i++ {
		menu := menus.Index(i)
		if menu.Get("dataset").Get("messageMenu").String() == id {
			return menu
		}
	}
	return js.Null()
}

func focusMessageMenu(id string, focusFirst bool) {
	if id == "" {
		return
	}
	frame := js.Global().Get("requestAnimationFrame")
	if frame.Type() != js.TypeFunction {
		return
	}
	var callback js.Func
	attempts := 0
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		if menu := messageMenuPopup(id); menu.Truthy() {
			positionMessageMenu(menu)
			items := shownMenuItems(menu)
			if focusFirst && len(items) > 0 {
				items[0].Call("focus")
			}
			callback.Release()
			return nil
		}
		attempts++
		if attempts < 15 {
			frame.Invoke(callback)
		} else {
			callback.Release()
		}
		return nil
	})
	frame.Invoke(callback)
}

func restoreMessageMenuFocus(id string) {
	if id == "" {
		return
	}
	timer := js.Global().Get("setTimeout")
	if timer.Type() != js.TypeFunction {
		return
	}
	var callback js.Func
	attempts := 0
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		doc := js.Global().Get("document")
		if messageMenuPopup(id).Truthy() && attempts < 15 {
			attempts++
			timer.Invoke(callback, 0)
			return nil
		}
		if doc.Truthy() && doc.Get("querySelectorAll").Type() == js.TypeFunction {
			if messageMenuTriggerID == id && messageMenuTrigger.Truthy() && messageMenuTrigger.Get("isConnected").Bool() && messageMenuTrigger.Call("getClientRects").Get("length").Int() > 0 {
				messageMenuTrigger.Call("focus", map[string]any{"preventScroll": true})
				callback.Release()
				return nil
			}
			buttons := doc.Call("querySelectorAll", ".message-action[data-action=menu]")
			for i := 0; i < buttons.Get("length").Int(); i++ {
				button := buttons.Index(i)
				if button.Get("dataset").Get("id").String() == id {
					button.Call("focus", map[string]any{"preventScroll": true})
					break
				}
			}
		}
		callback.Release()
		return nil
	})
	timer.Invoke(callback, 0)
}

func moveMessageMenuFocus(event ui.Event, id string) bool {
	key := event.JSValue().Get("key").String()
	if key != "ArrowDown" && key != "ArrowUp" && key != "Home" && key != "End" {
		return false
	}
	menu := messageMenuPopup(id)
	if !menu.Truthy() {
		return false
	}
	target := event.JSValue().Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return false
	}
	within := target.Call("closest", ".message-menu")
	if !within.Equal(menu) {
		trigger := target.Call("closest", ".message-action[data-action=menu]")
		if !trigger.Truthy() || trigger.Get("dataset").Get("id").String() != id {
			return false
		}
	}
	items := shownMenuItems(menu)
	length := len(items)
	if length == 0 {
		return false
	}
	index := -1
	itemTarget := target.Call("closest", "[role=menuitem],[role=menuitemradio]")
	for i := 0; i < length; i++ {
		if items[i].Equal(itemTarget) {
			index = i
			break
		}
	}
	index = messageMenuNextIndex(key, index, length)
	items[index].Call("focus")
	return true
}

// eventAction resolves a delegated click to the nearest data-action element
// and returns its action and id. GoWebComponents matches event hooks by
// position, so the workspace registers a fixed handful of hooks once per
// render and every row, message action and dialog button routes through
// this lookup instead of carrying a hook of its own (see bindHandlers).
func eventAction(event ui.Event) (action, id, extra string) {
	target := event.JSValue().Get("target")
	if !target.Truthy() || target.Get("closest").IsUndefined() {
		return "", "", ""
	}
	el := target.Call("closest", "[data-action]")
	if !el.Truthy() {
		return "", "", ""
	}
	if el.Get("disabled").Truthy() {
		return "", "", ""
	}
	ds := el.Get("dataset")
	return railActionData(ds)
}

func railActionData(ds js.Value) (action, id, extra string) {
	action = ds.Get("action").String()
	if v := ds.Get("id"); v.Type() == js.TypeString {
		id = v.String()
	}
	if v := ds.Get("extra"); v.Type() == js.TypeString {
		extra = v.String()
	} else if v := ds.Get("emoji"); v.Type() == js.TypeString {
		extra = v.String()
	}
	return action, id, extra
}

// eventPane names the column whose resize handle has focus for a key event,
// or "" when the key landed anywhere else.
func eventPane(event ui.Event) string {
	target := event.JSValue().Get("target")
	if !target.Truthy() || target.Get("classList").IsUndefined() {
		return ""
	}
	if !target.Get("classList").Call("contains", "pane-handle").Bool() {
		return ""
	}
	if v := target.Get("dataset").Get("pane"); v.Type() == js.TypeString {
		return v.String()
	}
	return ""
}

// eventOnBackdrop reports whether the click landed on a dialog backdrop
// itself rather than on anything inside the dialog.
func eventOnBackdrop(event ui.Event) bool {
	target := event.JSValue().Get("target")
	if !target.Truthy() {
		return false
	}
	cl := target.Get("classList")
	return cl.Truthy() && cl.Call("contains", "chat-dialog-backdrop").Bool()
}

// domValue reads a form control's current value by id. Typed text lives in
// the box (never in a Value prop, see fieldsync_js.go), so a submit reads
// the box rather than a render-time copy that may be a keystroke behind.
func domValue(id string) string {
	el := js.Global().Get("document").Call("getElementById", id)
	if !el.Truthy() {
		return ""
	}
	v := el.Get("value")
	if v.Type() != js.TypeString {
		return ""
	}
	return v.String()
}

// domDraftValue is domValue for a composer, but only while the box still holds
// text of the conversation named by scope. A box that has not yet been given
// the next conversation's draft holds the previous one's, and reading it would
// file that text under the wrong conversation.
func domDraftValue(id, scope string) string {
	el := js.Global().Get("document").Call("getElementById", id)
	if !el.Truthy() {
		return ""
	}
	if owner := el.Call("getAttribute", draftScopeAttr); owner.Type() == js.TypeString && owner.String() != scope {
		return ""
	}
	if owner := el.Get("__chatDraftScope"); owner.Type() == js.TypeString && owner.String() != scope {
		return ""
	}
	return domValue(id)
}

// setDOMValue writes a form control's value directly; used to clear the
// composer the instant a message is sent, so the next keystroke starts clean
// even before the application's draft state has re-rendered.
func setDOMValue(id, value string) {
	el := js.Global().Get("document").Call("getElementById", id)
	if !el.Truthy() {
		return
	}
	el.Set("value", value)
	el.Set("__chatTyped", false)
	el.Set("__chatCleared", value == "")
	setComposerSendReady(id, value)
}

func setComposerSendReady(id, value string) {
	field := js.Global().Get("document").Call("getElementById", id)
	if !field.Truthy() || field.Get("closest").Type() != js.TypeFunction {
		return
	}
	form := field.Call("closest", "form")
	if !form.Truthy() {
		return
	}
	button := form.Call("querySelector", ".send-button")
	if button.Truthy() {
		ready := chatPolishSendReady(form.Call("getAttribute", "data-send-capable").String() == "true", value)
		button.Set("disabled", !ready)
		button.Call("setAttribute", "aria-disabled", boolString(!ready))
	}
}

func focusSectionCreate() {
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		details := js.Global().Get("document").Call("getElementById", "chat-section-create")
		if !details.Truthy() || !details.Get("open").Truthy() {
			callback.Release()
			return nil
		}
		input := js.Global().Get("document").Call("getElementById", "chat-new-section")
		if input.Truthy() {
			input.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
		}
		ensureSectionCreateVisible()
		callback.Release()
		return nil
	})
	js.Global().Call("requestAnimationFrame", callback)
}

// ensureSectionCreateVisible keeps the disclosure form inside the rail's own
// scrollport. It deliberately adjusts only .rail-scroll, so opening the form
// never moves the document or the conversation timeline.
func ensureSectionCreateVisible() {
	details := js.Global().Get("document").Call("getElementById", "chat-section-create")
	if !details.Truthy() || !details.Get("open").Truthy() {
		return
	}
	scroll := details.Call("closest", ".rail-scroll")
	if !scroll.Truthy() {
		return
	}
	form := details.Call("querySelector", ".section-create-form")
	if !form.Truthy() {
		return
	}
	viewport := scroll.Call("getBoundingClientRect")
	content := form.Call("getBoundingClientRect")
	amount := 0.0
	if content.Get("bottom").Float() > viewport.Get("bottom").Float() {
		amount = content.Get("bottom").Float() - viewport.Get("bottom").Float()
	} else if content.Get("top").Float() < viewport.Get("top").Float() {
		amount = content.Get("top").Float() - viewport.Get("top").Float()
	}
	if amount != 0 {
		scroll.Set("scrollTop", scroll.Get("scrollTop").Float()+amount+8)
	}
}

func closeSectionCreate(clear bool) {
	doc := js.Global().Get("document")
	details := doc.Call("getElementById", "chat-section-create")
	if !details.Truthy() {
		return
	}
	if clear {
		setDOMValue("chat-new-section", "")
	}
	SetChatDisclosureOpen(details, false)
	summary := details.Call("querySelector", "[data-chat-disclosure-toggle]")
	if summary.Truthy() {
		summary.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	}
}

func sectionCreateOpen() bool {
	details := js.Global().Get("document").Call("getElementById", "chat-section-create")
	return details.Truthy() && details.Get("open").Truthy()
}
