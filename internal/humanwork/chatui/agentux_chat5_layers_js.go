//go:build js && wasm

package chatui

import (
	"strconv"
	"strings"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func chatLayerRoot() js.Value {
	return js.Global().Get("document").Call("querySelector", ".chat-workspace")
}

func rememberChatLayerOpener(e ui.MouseEvent) {
	rememberChatLayerTarget(e.JSValue().Get("target"))
}

func rememberChatLayerTarget(target js.Value) {
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	button := target.Call("closest", "button[data-action]")
	if !button.Truthy() {
		return
	}
	action := button.Get("dataset").Get("action").String()
	if kind := chatLayerKind(action); kind != "" {
		root := button.Call("closest", ".chat-workspace")
		if root.Truthy() {
			root.Set("__chatLayerOpener", button)
			root.Set("__chatLayerOpener_"+kind, button)
			root.Set("__chatLayerOpenerAction_"+kind, action)
			root.Set("__chatLayerOpenerID_"+kind, button.Get("dataset").Get("id"))
			root.Set("__chatLayerOpenerInBar_"+kind, button.Call("closest", ".message-actions").Truthy())
			// The hover bar is drawn only while its row is active, so the button can be
			// gone by the time the layer is placed; its rectangle at press time is the
			// anchor of last resort.
			rect := chatLayerAnchor(button, chatLayerAbove(kind)).Call("getBoundingClientRect")
			root.Set("__chatLayerRect_"+kind, []any{rect.Get("left").Float(), rect.Get("top").Float(), rect.Get("right").Float(), rect.Get("bottom").Float()})
		}
	}
}

func restoreChatLayerFocus(kinds ...string) {
	focusOpener := func() {
		root := chatLayerRoot()
		if root.Truthy() {
			opener := root.Get("__chatLayerOpener")
			if len(kinds) > 0 {
				opener = currentChatLayerOpener(root, kinds[0])
			}
			if opener.Truthy() && opener.Get("isConnected").Truthy() {
				opener.Call("focus", map[string]any{"preventScroll": true})
			}
		}
	}
	focusOpener()
	if len(kinds) > 0 && kinds[0] == "reaction" {
		// Closing the picker re-renders the message; once that has happened the
		// button may be a new node, and focus must land on it.
		var frame js.Func
		frame = js.FuncOf(func(js.Value, []js.Value) any {
			defer frame.Release()
			// A click on the composer between Escape and this frame owns focus now.
			if chatFocusRestoreAllowed(chatFocusHeldNow()) {
				focusOpener()
			}
			return nil
		})
		js.Global().Call("requestAnimationFrame", frame)
	}
}

func rememberedChatLayerRect(root js.Value, kind string) chatLayerRect {
	if !root.Truthy() {
		return chatLayerRect{}
	}
	value := root.Get("__chatLayerRect_" + kind)
	if value.Type() != js.TypeObject || value.Get("length").Int() != 4 {
		return chatLayerRect{}
	}
	return chatLayerRect{value.Index(0).Float(), value.Index(1).Float(), value.Index(2).Float(), value.Index(3).Float()}
}

// chatLayerAnchor is the element a layer is placed against: the message's
// action bar (or the whole message, or the composer) for layers that open above
// it, and a reaction chip itself, since the row around a chip is far wider and
// taller than the button that was pressed.
func chatLayerAnchor(opener js.Value, above bool) js.Value {
	anchor := opener
	if above && !opener.Call("matches", ".reaction").Bool() {
		if region := opener.Call("closest", ".message,.thread-root,.thread-message,.chat-composer,.thread-composer"); region.Truthy() {
			anchor = region
			if bar := opener.Call("closest", ".message-actions"); bar.Truthy() {
				anchor = bar
			}
		}
	}
	return anchor
}

func positionChatLayer(layer, opener js.Value, above bool) {
	if !layer.Truthy() || layer.Call("hasAttribute", "data-"+chatLayerSelfPlacedAttr).Bool() {
		return
	}
	// A detached opener (its hover bar was redrawn) measures as all zeros, which
	// used to put the layer in a corner of the page; it is never measured.
	var live chatLayerRect
	if opener.Truthy() && opener.Get("isConnected").Truthy() {
		rect := chatLayerAnchor(opener, above).Call("getBoundingClientRect")
		live = chatLayerRect{rect.Get("left").Float(), rect.Get("top").Float(), rect.Get("right").Float(), rect.Get("bottom").Float()}
	}
	root := chatLayerRoot()
	kind := layer.Get("dataset").Get("chatLayer").String()
	anchorRect, anchored := chatLayerAnchorRect(live, rememberedChatLayerRect(root, kind))
	if layer.Get("showPopover").Type() == js.TypeFunction && !layer.Call("matches", ":popover-open").Bool() {
		layer.Call("showPopover")
		markChatLayerOrder(layer)
	}
	viewportWidth, viewportHeight := js.Global().Get("innerWidth").Float(), js.Global().Get("innerHeight").Float()
	style := layer.Get("style")
	if chatLayerWritesGeneralWidth(kind, anchored) {
		style.Call("setProperty", "width", strconv.FormatFloat(chatLayerWidth(kind, viewportWidth), 'f', 2, 64)+"px")
	}
	if !anchored {
		return
	}
	rtl := root.Truthy() && root.Get("dir").String() == "rtl"
	var placement chatLayerPlacement
	if chatLayerGroup(kind) == chatSidebarSettingsGroup {
		// A sidebar footer panel sits above its own row and inside the sidebar.
		column := chatLayerRect{0, 0, viewportWidth, viewportHeight}
		rail := js.Undefined()
		if opener.Truthy() && opener.Get("isConnected").Truthy() {
			rail = opener.Call("closest", ".chat-rail")
		}
		if !rail.Truthy() && root.Truthy() {
			rail = root.Call("querySelector", ".chat-rail")
		}
		if rail.Truthy() {
			box := rail.Call("getBoundingClientRect")
			column = chatLayerRect{box.Get("left").Float(), box.Get("top").Float(), box.Get("right").Float(), box.Get("bottom").Float()}
		}
		placement = chatSidebarLayerPlacement(anchorRect, column, viewportWidth, viewportHeight, rtl)
	} else {
		width := chatLayerWidth(kind, viewportWidth)
		// What the layer needs to show all it holds, border included; it only
		// chooses the side, the layer is not capped at it.
		need := chatLayerContentHeight(layer.Get("scrollHeight").Float(), layer.Get("offsetHeight").Float(), layer.Get("clientHeight").Float())
		placement = chatLayerPlace(anchoredChatGeometry(anchorRect, width, need, viewportWidth, viewportHeight, above, rtl), anchorRect, viewportHeight)
	}
	pixels := func(value float64) string { return strconv.FormatFloat(value, 'f', 2, 64) + "px" }
	style.Call("setProperty", "left", pixels(placement.left))
	style.Call("setProperty", "width", pixels(placement.width))
	style.Call("setProperty", "max-height", pixels(placement.maxHeight))
	if placement.up {
		style.Call("setProperty", "top", "auto")
		style.Call("setProperty", "bottom", pixels(placement.bottom))
	} else {
		style.Call("setProperty", "top", pixels(placement.top))
		style.Call("setProperty", "bottom", "auto")
	}
	style.Call("setProperty", "position", "fixed")
	style.Call("setProperty", "inset-inline-end", "auto")
	style.Call("setProperty", "transform", "none")
}

func syncChatAnchoredLayers() {
	root := chatLayerRoot()
	if !root.Truthy() {
		return
	}
	chatux007SyncJump()
	opener := root.Get("__chatLayerOpener")
	layers := root.Call("querySelectorAll", "[data-chat-layer]:not([hidden])")
	// A dialog and the panels over it do not mix: a top-layer panel is drawn above
	// the dialog's scrim and its page.
	dialogOpen := root.Call("querySelector", chatDialogSelector).Truthy()
	for i := 0; i < layers.Get("length").Int(); i++ {
		layer := layers.Index(i)
		kind := layer.Get("dataset").Get("chatLayer").String()
		if kind == "saved" || layer.Call("hasAttribute", "data-"+chatLayerSelfPlacedAttr).Bool() {
			continue
		}
		if dialogOpen && chatLayerOutsideDismisses(kind) {
			closeChatDisclosureLayer(layer, false)
			continue
		}
		anchor := currentChatLayerOpener(root, kind)
		// A panel that belongs to a disclosure is anchored to that disclosure's own
		// button as it is now, not to whichever button was pressed last.
		if layer.Call("hasAttribute", "data-chat-disclosure-body").Bool() {
			if disclosure := layer.Call("closest", "[data-chat-disclosure]"); disclosure.Truthy() {
				if toggle := disclosure.Call("querySelector", "[data-chat-disclosure-toggle]"); toggle.Truthy() {
					anchor = toggle
				}
			}
		}
		if !anchor.Truthy() || !anchor.Get("isConnected").Truthy() {
			// A reaction picker has no selector: it belongs to one message, and the first
			// "react-pick" button on the page is another message's.
			selector := map[string]string{"search": "[data-action=chat-search-open]", "emoji": "[data-action=emoji-toggle]", "menu": "[data-action=menu]"}[kind]
			if selector != "" {
				anchor = root.Call("querySelector", selector)
			}
			if kind != "reaction" && !anchor.Truthy() {
				anchor = opener
			}
		}
		positionChatLayer(layer, anchor, chatLayerAbove(kind))
		if kind == "reaction" || kind == "emoji" {
			fillChatRecentEmoji(root, layer)
		}
	}
}

func chatLayerContainsEvent(e ui.MouseEvent) bool {
	target := e.JSValue().Get("target")
	return target.Truthy() && target.Get("closest").Type() == js.TypeFunction && target.Call("closest", "[data-chat-layer]").Truthy()
}

func focusChatLayerField(id string) {
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		defer frame.Release()
		if field := js.Global().Get("document").Call("getElementById", id); field.Truthy() {
			field.Call("focus")
		}
		return nil
	})
	js.Global().Call("requestAnimationFrame", frame)
}

func chatRecentEmoji(root js.Value) []string {
	var recent []string
	if values := root.Get("__chatRecentEmoji"); values.Truthy() {
		for i := 0; i < values.Get("length").Int(); i++ {
			recent = append(recent, values.Index(i).String())
		}
	}
	return recent
}

func fillChatRecentEmoji(root, picker js.Value) {
	grid := picker.Call("querySelector", `[data-emoji-category="chat.emoji.recent"] .emoji-grid`)
	if !grid.Truthy() {
		return
	}
	recent := chatRecentEmoji(root)
	key := strings.Join(recent, "\x00")
	if old := grid.Get("__chatRecentKey"); old.Type() == js.TypeString && old.String() == key {
		return
	}
	grid.Set("__chatRecentKey", key)
	grid.Set("textContent", "")
	choices := picker.Call("querySelectorAll", `.emoji-category:not([data-emoji-category="chat.emoji.recent"]) [data-emoji]`)
	for _, emoji := range recent {
		for i := 0; i < choices.Get("length").Int(); i++ {
			choice := choices.Index(i)
			if choice.Get("dataset").Get("emoji").String() == emoji {
				grid.Call("appendChild", choice.Call("cloneNode", true))
				break
			}
		}
	}
}

func focusChatAgentsSection() {
	var frame js.Func
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		defer frame.Release()
		if section := js.Global().Get("document").Call("getElementById", "chat-agents-here"); section.Truthy() {
			section.Call("focus")
			section.Call("scrollIntoView", map[string]any{"block": "nearest"})
		}
		return nil
	})
	js.Global().Call("requestAnimationFrame", frame)
}

func bindChatActiveRows(local localStore) func() {
	root := chatLayerRoot()
	if !root.Truthy() {
		return nil
	}
	rowID := func(target js.Value) string {
		if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
			return ""
		}
		if row := target.Call("closest", ".message[data-message-id],.thread-message[data-message-id]"); row.Truthy() {
			return row.Get("dataset").Get("messageId").String()
		}
		return ""
	}
	listeners := map[string]js.Func{}
	for _, name := range []string{"pointerover", "pointerout", "focusin", "focusout"} {
		name := name
		listener := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			event := args[0]
			target := event.Get("target")
			if name == "pointerover" && event.Get("pointerType").String() == "touch" {
				return nil
			}
			// A phone-width viewport or a touch screen: actions follow the row that was
			// touched or focused, never a mouse that is merely resting over a row.
			compact := js.Global().Call("matchMedia", "(max-width:767px), (pointer:coarse)").Get("matches").Bool()
			id := rowID(target)
			if name == "pointerout" || name == "focusout" {
				id = rowID(event.Get("relatedTarget"))
			}
			state := local.get()
			if strings.HasPrefix(name, "pointer") {
				if !chatRowPointerActivates(event.Get("pointerType").String(), compact) {
					id = ""
				}
				if state.pointerRow != id {
					local.update(func(u *localUI) { u.pointerRow = id })
				}
			} else {
				focusTarget := target
				if name == "focusout" {
					focusTarget = event.Get("relatedTarget")
				}
				focusVisible := focusTarget.Truthy() && focusTarget.Get("matches").Type() == js.TypeFunction && focusTarget.Call("matches", ":focus-visible").Bool()
				if !chatRowFocusActivates(focusVisible, compact) {
					id = ""
				}
				if state.focusRow != id {
					local.update(func(u *localUI) { u.focusRow = id })
				}
			}
			return nil
		})
		listeners[name] = listener
		root.Call("addEventListener", name, listener)
	}
	search := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		input := args[0].Get("target")
		if !input.Truthy() || !input.Get("classList").Call("contains", "emoji-search").Bool() {
			return nil
		}
		picker := input.Call("closest", "[data-chat-layer]")
		choices := picker.Call("querySelectorAll", "[data-emoji]")
		query := strings.ToLower(strings.TrimSpace(input.Get("value").String()))
		for i := 0; i < choices.Get("length").Int(); i++ {
			choice := choices.Index(i)
			choice.Set("hidden", !strings.Contains(strings.ToLower(choice.Get("dataset").Get("emojiName").String()+choice.Get("textContent").String()), query))
		}
		return nil
	})
	root.Call("addEventListener", "input", search)
	recent := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
			return nil
		}
		choice := target.Call("closest", "[data-emoji]")
		if !choice.Truthy() {
			return nil
		}
		values := chat5RecentEmoji(chatRecentEmoji(root), choice.Get("dataset").Get("emoji").String())
		array := js.Global().Get("Array").New()
		for _, value := range values {
			array.Call("push", value)
		}
		root.Set("__chatRecentEmoji", array)
		return nil
	})
	root.Call("addEventListener", "click", recent, true)
	capture := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		rememberChatLayerTarget(target)
		if target.Truthy() && target.Get("closest").Type() == js.TypeFunction {
			if button := target.Call("closest", "[data-chat-disclosure-toggle]"); button.Truthy() && !button.Get("disabled").Truthy() {
				toggleChatDisclosure(root, button)
			}
		}
		return nil
	})
	root.Call("addEventListener", "click", capture, true)
	closeLayer := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || args[0].Get("key").String() != "Escape" {
			return nil
		}
		closeTopChatLayerOnEscape(root, args[0])
		return nil
	})
	root.Call("addEventListener", "keydown", closeLayer, true)
	// Focus is not always inside the workspace: after a click on a bar that is
	// redrawn, or in a window that delivers no focus events, it sits on the page
	// itself and the workspace never hears the key. The document hears it and
	// gives it to the workspace.
	doc := js.Global().Get("document")
	documentKey := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Get("key").String() == "Escape" {
			escapeChatLayersFromPage(args[0])
		}
		return nil
	})
	doc.Call("addEventListener", "keydown", documentKey, true)
	// A click elsewhere closes a sidebar panel; it never takes focus from what
	// was clicked.
	documentClick := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			dismissChatLayersOnOutsideClick(args[0].Get("target"))
		}
		return nil
	})
	doc.Call("addEventListener", "click", documentClick, true)
	geometry := js.FuncOf(func(js.Value, []js.Value) any { syncChatAnchoredLayers(); return nil })
	js.Global().Call("addEventListener", "resize", geometry)
	js.Global().Call("addEventListener", "scroll", geometry, true)
	// A layer whose content changes after it opened (a list that finished
	// loading, a disclosure inside it) or whose element a render replaced is
	// measured and shown again. Observer callbacks are microtasks, so this does
	// not wait for a timer.
	settling := false
	settle := js.FuncOf(func(js.Value, []js.Value) any {
		if settling {
			return nil
		}
		settling = true
		defer func() { settling = false }()
		syncChatAnchoredLayers()
		return nil
	})
	observer := js.Global().Get("MutationObserver").New(settle)
	observer.Call("observe", root, map[string]any{"childList": true, "subtree": true, "attributes": true, "attributeFilter": []any{"hidden", "disabled"}})
	return func() {
		observer.Call("disconnect")
		settle.Release()
		doc.Call("removeEventListener", "keydown", documentKey, true)
		documentKey.Release()
		doc.Call("removeEventListener", "click", documentClick, true)
		documentClick.Release()
		for name, listener := range listeners {
			root.Call("removeEventListener", name, listener)
			listener.Release()
		}
		root.Call("removeEventListener", "input", search)
		search.Release()
		root.Call("removeEventListener", "click", capture, true)
		capture.Release()
		root.Call("removeEventListener", "keydown", closeLayer, true)
		closeLayer.Release()
		root.Call("removeEventListener", "click", recent, true)
		recent.Release()
		js.Global().Call("removeEventListener", "resize", geometry)
		js.Global().Call("removeEventListener", "scroll", geometry, true)
		geometry.Release()
	}
}

const chatFocusableSelector = "button,a[href],input,select,textarea,[tabindex]"

// toggleChatDisclosure opens or closes one disclosure at once. Nothing here waits
// for a frame or a timer: a window that throttles them must still show the body.
func toggleChatDisclosure(root, button js.Value) {
	disclosure := button.Call("closest", "[data-chat-disclosure]")
	if !disclosure.Truthy() {
		return
	}
	body := disclosure.Call("querySelector", "[data-chat-disclosure-body]")
	if !body.Truthy() {
		return
	}
	opening := body.Get("hidden").Bool()
	body.Set("hidden", !opening)
	disclosure.Set("open", opening)
	disclosure.Call("toggleAttribute", "open", opening)
	button.Call("setAttribute", "aria-expanded", strconv.FormatBool(opening))
	if body.Call("hasAttribute", "data-chat-layer").Bool() {
		if opening {
			// One panel of a group at a time, and the opener is remembered under the
			// panel's own kind so a later render finds its anchor again.
			closeChatLayersOfKind(body)
			kind := body.Get("dataset").Get("chatLayer").String()
			box := button.Call("getBoundingClientRect")
			root.Set("__chatLayerOpener_"+kind, button)
			root.Set("__chatLayerRect_"+kind, []any{box.Get("left").Float(), box.Get("top").Float(), box.Get("right").Float(), box.Get("bottom").Float()})
			positionChatLayer(body, button, false)
		} else if body.Call("matches", ":popover-open").Bool() {
			body.Call("hidePopover")
		}
	}
	if opening && !button.Call("hasAttribute", "data-chatvoice-expand").Bool() {
		// Put the person inside what they opened: the first control takes focus, and
		// focusing it scrolls it into view in the list or popover that holds it.
		if control := body.Call("querySelector", "input:not([disabled]):not([type=hidden]),select:not([disabled]),textarea:not([disabled]),button:not([disabled]),a[href]"); control.Truthy() {
			control.Call("focus")
		}
	}
	// The popover that holds this disclosure grows or shrinks with it.
	syncChatAnchoredLayers()
}

// closeChatDisclosureLayer closes a panel that belongs to a disclosure and keeps
// the disclosure's button in step with it.
func closeChatDisclosureLayer(layer js.Value, focusToggle bool) {
	layer.Set("hidden", true)
	if layer.Call("matches", ":popover-open").Bool() {
		layer.Call("hidePopover")
	}
	if disclosure := layer.Call("closest", "[data-chat-disclosure]"); disclosure.Truthy() {
		disclosure.Set("open", false)
		disclosure.Call("removeAttribute", "open")
		if button := disclosure.Call("querySelector", "[data-chat-disclosure-toggle]"); button.Truthy() {
			button.Call("setAttribute", "aria-expanded", "false")
			if focusToggle {
				button.Call("focus")
			}
		}
	}
}

// closeTopChatLayerOnEscape closes the topmost managed layer, and only that one,
// and reports whether it did.
func closeTopChatLayerOnEscape(root, event js.Value) bool {
	layer := topChatLayer(root)
	if !layer.Truthy() {
		return false
	}
	kind := layer.Get("dataset").Get("chatLayer").String()
	if !chatPolishManagedLayer(kind) {
		return false
	}
	switch kind {
	case "saved":
		if close := layer.Call("querySelector", "[data-saved-action=close]"); close.Truthy() {
			close.Call("click")
		}
	case "location":
		CloseLocationComposer(layer, true)
	case "writing-style":
		if opener := root.Get("__chatLayerOpener_writing-style"); opener.Truthy() {
			CloseWritingStyleComposer(opener, true)
		}
	case "voice":
		if opener := root.Get("__chatLayerOpener_voice"); opener.Truthy() {
			CloseVoiceComposer(opener, true)
		}
	default:
		closeChatDisclosureLayer(layer, true)
	}
	event.Call("preventDefault")
	event.Call("stopImmediatePropagation")
	return true
}

// escapeChatLayersFromPage handles Escape when nothing inside the workspace has
// focus (the page itself does). A managed layer is closed here; anything else
// that is open is handed to the workspace's own Escape handling as if the key
// had been pressed inside it.
func escapeChatLayersFromPage(event js.Value) {
	root := chatLayerRoot()
	doc := js.Global().Get("document")
	if !root.Truthy() || !doc.Truthy() {
		return
	}
	target := event.Get("target")
	if target.Truthy() && !target.Equal(doc) && !target.Equal(doc.Get("body")) && !target.Equal(doc.Get("documentElement")) {
		// Focus is on some control: inside the workspace its handlers have the key,
		// and outside it the key is not ours.
		return
	}
	if closeTopChatLayerOnEscape(root, event) {
		return
	}
	// The emoji picker answers Escape itself (a typed query first, then closing):
	// forwarding it to the workspace would close something behind it as well.
	if kind := chatTopLayerKind(); kind == "emoji" || kind == "reaction" {
		return
	}
	if !root.Call("querySelector", "[data-chat-layer]:not([hidden]),"+chatDialogSelector).Truthy() {
		return
	}
	forwarded := js.Global().Get("KeyboardEvent").New("keydown", map[string]any{"key": "Escape", "code": "Escape", "bubbles": true, "cancelable": true})
	root.Call("dispatchEvent", forwarded)
	event.Call("preventDefault")
	event.Call("stopPropagation")
}

// dismissChatLayersOnOutsideClick closes the open sidebar panels that the click
// did not land in. Focus returns to a panel's row only when the click landed on
// nothing that takes focus; a click on the composer keeps the composer.
func dismissChatLayersOnOutsideClick(target js.Value) {
	root := chatLayerRoot()
	if !root.Truthy() {
		return
	}
	layers := root.Call("querySelectorAll", "[data-chat-layer]:not([hidden])")
	for i := 0; i < layers.Get("length").Int(); i++ {
		layer := layers.Index(i)
		if !chatLayerOutsideDismisses(layer.Get("dataset").Get("chatLayer").String()) {
			continue
		}
		disclosure := layer.Call("closest", "[data-chat-disclosure]")
		if target.Truthy() && (layer.Call("contains", target).Bool() || (disclosure.Truthy() && disclosure.Call("contains", target).Bool())) {
			continue
		}
		closeChatDisclosureLayer(layer, false)
		takesFocus := target.Truthy() && target.Get("closest").Type() == js.TypeFunction && target.Call("closest", chatFocusableSelector).Truthy()
		if !takesFocus && disclosure.Truthy() {
			if button := disclosure.Call("querySelector", "[data-chat-disclosure-toggle]"); button.Truthy() {
				button.Call("focus", map[string]any{"preventScroll": true})
			}
		}
	}
}

// chatFocusHeldNow reports what holds focus at this moment, for chatFocusRestoreAllowed.
func chatFocusHeldNow() chatFocusHolder {
	doc := js.Global().Get("document")
	active := doc.Get("activeElement")
	return chatFocusHolder{
		Idle:      !active.Truthy() || active.Equal(doc.Get("body")) || active.Equal(doc.Get("documentElement")),
		Connected: active.Truthy() && active.Get("isConnected").Truthy(),
	}
}

// ToggleVoiceComposer uses the same viewport-constrained layer geometry as Chat's other tools.
func ToggleVoiceComposer(button js.Value) {
	root := button.Call("closest", "[data-chatvoice-recorder]")
	if !root.Truthy() {
		return
	}
	panel := root.Call("querySelector", "[data-chat-layer='voice']")
	if !panel.Truthy() {
		return
	}
	opening := panel.Get("hidden").Bool()
	panel.Set("hidden", !opening)
	button.Call("setAttribute", "aria-expanded", strconv.FormatBool(opening))
	if opening {
		workspace := chatLayerRoot()
		if workspace.Truthy() {
			workspace.Set("__chatLayerOpener_voice", button)
		}
		closeChatLayersOfKind(panel)
		positionChatLayer(panel, button, true)
		if field := panel.Call("querySelector", "button:not([disabled])"); field.Truthy() {
			field.Call("focus")
		}
	} else {
		CloseVoiceComposer(button, true)
	}
}

func CloseVoiceComposer(button js.Value, restoreFocus bool) {
	root := button.Call("closest", "[data-chatvoice-recorder]")
	if !root.Truthy() {
		return
	}
	panel := root.Call("querySelector", "[data-chat-layer=voice]")
	if panel.Truthy() {
		panel.Set("hidden", true)
		if panel.Call("matches", ":popover-open").Bool() {
			panel.Call("hidePopover")
		}
	}
	button.Call("setAttribute", "aria-expanded", "false")
	if restoreFocus {
		button.Call("focus")
	}
}

// SetChatDisclosureOpen keeps the disclosure button and body in the same state.
func SetChatDisclosureOpen(disclosure js.Value, open bool) {
	if !disclosure.Truthy() {
		return
	}
	disclosure.Set("open", open)
	disclosure.Call("toggleAttribute", "open", open)
	if body := disclosure.Call("querySelector", "[data-chat-disclosure-body]"); body.Truthy() {
		body.Set("hidden", !open)
	}
	if button := disclosure.Call("querySelector", "[data-chat-disclosure-toggle]"); button.Truthy() {
		button.Call("setAttribute", "aria-expanded", strconv.FormatBool(open))
	}
}

func ToggleWritingStyleComposer(button js.Value) {
	panel := button.Call("closest", "[data-chattone=toolbar]").Call("querySelector", "[data-chat-layer=writing-style]")
	opening := panel.Get("hidden").Bool()
	panel.Set("hidden", !opening)
	button.Call("setAttribute", "aria-expanded", strconv.FormatBool(opening))
	if opening {
		if root := chatLayerRoot(); root.Truthy() {
			root.Set("__chatLayerOpener_writing-style", button)
		}
		positionChatLayer(panel, button, true)
		if field := panel.Call("querySelector", "button:not([disabled])"); field.Truthy() {
			field.Call("focus")
		} else {
			panel.Set("tabIndex", -1)
			panel.Call("focus")
		}
	} else {
		button.Call("focus")
	}
}

func OpenWritingStyleComposer(button js.Value) {
	panel := button.Call("closest", "[data-chattone=toolbar]").Call("querySelector", "[data-chat-layer=writing-style]")
	closeChatLayersOfKind(panel)
	panel.Set("hidden", false)
	controls := button.Call("closest", "[data-chattone=toolbar]").Call("querySelectorAll", "[data-chattone-action=rewrite]")
	for i := 0; i < controls.Length(); i++ {
		controls.Index(i).Call("setAttribute", "aria-expanded", "false")
	}
	button.Call("setAttribute", "aria-expanded", "true")
	if root := chatLayerRoot(); root.Truthy() {
		root.Set("__chatLayerOpener_writing-style", button)
	}
	positionChatLayer(panel, button, true)
}

func CloseWritingStyleComposer(button js.Value, restoreFocus bool) {
	panel := button.Call("closest", "[data-chattone=toolbar]").Call("querySelector", "[data-chat-layer=writing-style]")
	panel.Set("hidden", true)
	if panel.Call("matches", ":popover-open").Bool() {
		panel.Call("hidePopover")
	}
	controls := button.Call("closest", "[data-chattone=toolbar]").Call("querySelectorAll", "[data-chattone-action=rewrite]")
	for i := 0; i < controls.Length(); i++ {
		controls.Index(i).Call("setAttribute", "aria-expanded", "false")
	}
	if restoreFocus {
		if root := chatLayerRoot(); root.Truthy() {
			if opener := root.Get("__chatLayerOpener_writing-style"); opener.Truthy() {
				opener.Call("focus")
			}
		}
	}
}

func ToggleLocationComposer(button js.Value) {
	panel := button.Get("parentElement").Call("querySelector", "[data-chat-layer=location]")
	if !panel.Truthy() {
		return
	}
	if !panel.Get("hidden").Bool() {
		CloseLocationComposer(panel, true)
		return
	}
	panels := js.Global().Get("document").Call("querySelectorAll", "[data-chat-layer=location]:not([hidden])")
	for i := 0; i < panels.Length(); i++ {
		CloseLocationComposer(panels.Index(i), false)
	}
	panel.Set("hidden", false)
	button.Call("setAttribute", "aria-expanded", "true")
	if root := chatLayerRoot(); root.Truthy() {
		root.Set("__chatLayerOpener_location", button)
	}
	positionChatLayer(panel, button, true)
	if field := panel.Call("querySelector", "button:not([disabled])"); field.Truthy() {
		field.Call("focus")
	}
}

func CloseLocationComposer(panel js.Value, restoreFocus bool) {
	panel.Set("hidden", true)
	if panel.Get("hidePopover").Type() == js.TypeFunction && panel.Call("matches", ":popover-open").Bool() {
		panel.Call("hidePopover")
	}
	if button := panel.Get("parentElement").Call("querySelector", "[data-chatmap-action=toggle]"); button.Truthy() {
		button.Call("setAttribute", "aria-expanded", "false")
		if restoreFocus {
			button.Call("focus")
		}
	}
}

func markChatLayerOrder(layer js.Value) {
	root := chatLayerRoot()
	if !root.Truthy() {
		return
	}
	order := 1
	if previous := root.Get("__chatLayerOrder"); previous.Type() == js.TypeNumber {
		order = previous.Int() + 1
	}
	root.Set("__chatLayerOrder", order)
	layer.Set("__chatLayerOrder", order)
}

func topChatLayer(root js.Value) js.Value {
	nodes := root.Call("querySelectorAll", "[data-chat-layer]:not([hidden])")
	layers := []chatPolishLayer{}
	for i := 0; i < nodes.Length(); i++ {
		node := nodes.Index(i)
		order := 0
		if stamp := node.Get("__chatLayerOrder"); stamp.Type() == js.TypeNumber {
			order = stamp.Int()
		}
		layers = append(layers, chatPolishLayer{kind: node.Get("dataset").Get("chatLayer").String(), order: order, visible: node.Call("matches", ":popover-open").Bool()})
	}
	if top := chatPolishTopLayer(layers); top >= 0 {
		return nodes.Index(top)
	}
	return js.Undefined()
}

func ChatLayerIsTop(layer js.Value) bool {
	root := chatLayerRoot()
	if !root.Truthy() {
		return false
	}
	top := topChatLayer(root)
	return top.Truthy() && top.Equal(layer)
}

func OpenSavedMessagesLayer(layer, button js.Value) {
	layer.Set("hidden", false)
	if layer.Get("showPopover").Type() == js.TypeFunction && !layer.Call("matches", ":popover-open").Bool() {
		layer.Call("showPopover")
	}
	markChatLayerOrder(layer)
	button.Call("setAttribute", "aria-expanded", "true")
}

func closeChatLayersOfKind(layer js.Value) {
	root := chatLayerRoot()
	if !root.Truthy() {
		return
	}
	kind := layer.Get("dataset").Get("chatLayer").String()
	nodes := root.Call("querySelectorAll", "[data-chat-layer]:not([hidden])")
	open := make([]string, nodes.Length())
	for i := range open {
		if node := nodes.Index(i); !node.Equal(layer) {
			open[i] = node.Get("dataset").Get("chatLayer").String()
		}
	}
	// The layers of its own kind, and of its group: Quiet hours and Reading
	// languages are never open together.
	for _, i := range chatLayersToClose(open, kind) {
		old := nodes.Index(i)
		old.Set("hidden", true)
		if old.Call("matches", ":popover-open").Bool() {
			old.Call("hidePopover")
		}
		if disclosure := old.Call("closest", "[data-chat-disclosure]"); disclosure.Truthy() {
			disclosure.Set("open", false)
			disclosure.Call("removeAttribute", "open")
			disclosure.Call("querySelector", "button").Call("setAttribute", "aria-expanded", "false")
		}
	}
}

func currentChatLayerOpener(root js.Value, kind string) js.Value {
	opener := root.Get("__chatLayerOpener_" + kind)
	if opener.Truthy() && opener.Get("isConnected").Truthy() {
		return opener
	}
	action := root.Get("__chatLayerOpenerAction_" + kind)
	id := root.Get("__chatLayerOpenerID_" + kind)
	if action.Type() == js.TypeString {
		buttons := root.Call("querySelectorAll", "button[data-action]")
		var found []js.Value
		var inBar, usable []bool
		for i := 0; i < buttons.Length(); i++ {
			button := buttons.Index(i)
			if button.Get("dataset").Get("action").String() == action.String() && button.Get("dataset").Get("id").Equal(id) {
				box := button.Call("getBoundingClientRect")
				found = append(found, button)
				inBar = append(inBar, button.Call("closest", ".message-actions").Truthy())
				usable = append(usable, box.Get("width").Float() > 0 && box.Get("height").Float() > 0)
			}
		}
		if len(found) > 0 {
			// A message's hover bar and its reaction chips share an action and id: take the
			// one where the user pressed, never one that is not laid out.
			if pick := chatLayerPickOpener(inBar, usable, root.Get("__chatLayerOpenerInBar_"+kind).Truthy()); pick >= 0 {
				return found[pick]
			}
			return found[0]
		}
	}
	return opener
}

func chatTopLayerKind() string {
	root := chatLayerRoot()
	if root.Truthy() {
		if layer := topChatLayer(root); layer.Truthy() {
			return layer.Get("dataset").Get("chatLayer").String()
		}
	}
	return ""
}
