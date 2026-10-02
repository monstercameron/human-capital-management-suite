//go:build js && wasm

package chatui

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The browser half of the emoji picker: listeners that need no focus or blur
// event and no timer, the geometry of the floating layer, the person's saved
// preferences, and the fetch of the data files. The review pane delivers no focus
// events and throttles timers, so nothing here waits for either: every behaviour
// is driven by the click, key, input, scroll or pointer event that asked for it,
// or by the render that follows a state change.

func emojiDocument() js.Value { return js.Global().Get("document") }

// chatEmojiPickerElement is the open picker's element, or an undefined value.
func chatEmojiPickerElement() js.Value {
	doc := emojiDocument()
	if !doc.Truthy() {
		return js.Undefined()
	}
	return doc.Call("querySelector", "[data-emoji-picker]")
}

func chatEmojiSearchField(picker js.Value) js.Value {
	if !picker.Truthy() {
		return js.Undefined()
	}
	return picker.Call("querySelector", ".emoji-pop-input")
}

func emojiFocusOptions() js.Value { return js.ValueOf(map[string]any{"preventScroll": true}) }

// chatEmojiEnvironment reads, once when the picker opens, whether the screen is
// touch (larger cells) and whether the viewport is phone width (a sheet).
func chatEmojiEnvironment() (touch, sheet bool) {
	win := js.Global()
	if match := win.Call("matchMedia", "(pointer:coarse)"); match.Truthy() {
		touch = match.Get("matches").Truthy()
	}
	return touch, win.Get("innerWidth").Float() <= 767
}

// chatEmojiFlagState caches the answer to chatEmojiPlatformDrawsFlags: 0 not asked
// yet, 1 the platform draws flags, 2 it does not.
var chatEmojiFlagState int

// chatEmojiPlatformDrawsFlags asks, once, whether this platform draws flag emoji:
// a flag drawn as one glyph is narrower than its two regional-indicator letters
// drawn apart, and a platform with no flag glyphs draws the pair as those two
// letters. Nothing is downloaded; an unreadable canvas counts as "draws them".
func chatEmojiPlatformDrawsFlags() bool {
	if chatEmojiFlagState != 0 {
		return chatEmojiFlagState == 1
	}
	chatEmojiFlagState = 1
	doc := emojiDocument()
	if !doc.Truthy() {
		return true
	}
	canvas := doc.Call("createElement", "canvas")
	ctx := js.Undefined()
	if canvas.Truthy() && canvas.Get("getContext").Type() == js.TypeFunction {
		ctx = canvas.Call("getContext", "2d")
	}
	if !ctx.Truthy() {
		return true
	}
	ctx.Set("font", "32px \"Segoe UI Emoji\",\"Apple Color Emoji\",\"Noto Color Emoji\",sans-serif")
	width := func(text string) float64 { return ctx.Call("measureText", text).Get("width").Float() }
	whole, apart := width("\U0001F1FA\U0001F1F8"), width("\U0001F1FA")+width("\U0001F1F8")
	if apart > 0 && whole >= apart-1 {
		chatEmojiFlagState = 2
	}
	return chatEmojiFlagState == 1
}

// chatEmojiStartLoad fetches the data files in the background; the picker shows
// its frequently used row meanwhile and fills in when they arrive.
func chatEmojiStartLoad(lang string) {
	loader := emojiLoader{Origin: js.Global().Get("location").Get("origin").String(), HTTP: &http.Client{Timeout: emojiLoadTimeout}}
	if island := emojiDocument().Call("getElementById", "journey-config"); island.Truthy() {
		var session struct {
			Bearer string `json:"bearer"`
		}
		if json.Unmarshal([]byte(island.Get("textContent").String()), &session) == nil {
			loader.Bearer = session.Bearer
		}
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), emojiLoadTimeout)
		defer cancel()
		ix, err := loader.Load(ctx, lang)
		chatEmojiLoaded(lang, ix, err)
	}()
}

// chatEmojiClick is the delegated click hook for the picker's own controls.
func chatEmojiClick(e ui.Event, action, id, extra string) bool {
	if !strings.HasPrefix(action, "emoji-") {
		return false
	}
	return chatEmojiAction(action, id, extra, e.JSValue().Get("shiftKey").Truthy())
}

// insertComposerEmoji puts an emoji into the composer at the caret. The field
// takes it as typed text, so a render cannot roll it back. With keepOpen the
// cursor goes back to the picker's search field, ready for the next one.
func insertComposerEmoji(targetID, emoji string, keepOpen bool) {
	field := emojiDocument().Call("getElementById", targetID)
	if !field.Truthy() || field.Get("disabled").Truthy() || emoji == "" {
		return
	}
	value := field.Get("value").String()
	updated, caret := insertEmojiAtUTF16(value, emoji, field.Get("selectionStart").Int(), field.Get("selectionEnd").Int())
	field.Set("value", updated)
	// This marks the field as user-typed and follows the same draft callback as
	// keyboard input, so a render cannot roll back a picker insertion.
	field.Call("dispatchEvent", js.Global().Get("Event").New("input", map[string]any{"bubbles": true}))
	field = emojiDocument().Call("getElementById", targetID)
	if !field.Truthy() {
		return
	}
	field.Call("setSelectionRange", caret, caret)
	if keepOpen {
		if search := chatEmojiSearchField(chatEmojiPickerElement()); search.Truthy() {
			search.Call("focus", emojiFocusOptions())
		}
		return
	}
	field.Call("focus", emojiFocusOptions())
	field.Call("setSelectionRange", caret, caret)
}

// handleEmojiPickerKey is not used: the picker's keys are taken by the listener
// bindChatEmoji installs, which runs before the workspace's own handlers.
func handleEmojiPickerKey(ui.KeyboardEvent) bool { return false }

func emojiSetField(field js.Value, value string) {
	if !field.Truthy() {
		return
	}
	field.Set("value", value)
	end := len([]rune(value))
	field.Call("focus", emojiFocusOptions())
	if field.Get("setSelectionRange").Type() == js.TypeFunction {
		field.Call("setSelectionRange", end, end)
	}
}

// bindChatEmoji installs the picker's listeners on the document, in the capture
// phase so they run before the workspace's own key handling.
func bindChatEmoji() func() {
	doc := emojiDocument()
	if !doc.Truthy() {
		return func() {}
	}
	type binding struct {
		name string
		fn   js.Func
	}
	var bindings []binding
	add := func(name string, handler func(event js.Value)) {
		fn := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				handler(args[0])
			}
			return nil
		})
		doc.Call("addEventListener", name, fn, true)
		bindings = append(bindings, binding{name, fn})
	}
	inPicker := func(target js.Value) bool {
		return target.Truthy() && target.Get("closest").Type() == js.TypeFunction && target.Call("closest", "[data-emoji-picker]").Truthy()
	}
	matches := func(target js.Value, selector string) bool {
		return target.Truthy() && target.Get("matches").Type() == js.TypeFunction && target.Call("matches", selector).Truthy()
	}

	add("input", func(event js.Value) {
		target := event.Get("target")
		if matches(target, ".emoji-pop-input") {
			chatEmojiSetQuery(target.Get("value").String())
		}
	})
	add("scroll", func(event js.Value) {
		target := event.Get("target")
		if matches(target, "[data-emoji-scroll]") {
			chatEmojiScrolled(target.Get("scrollTop").Float(), target.Get("clientHeight").Float())
			return
		}
		// The page behind scrolled: the picker follows its opener. The shared layer
		// code leaves it alone (data-chat-layer-self), so this is the only placement.
		if picker := chatEmojiPickerElement(); picker.Truthy() {
			if st, open := chatEmojiHost.current(); open {
				chatEmojiPlaceLayer(picker, st)
			}
		}
	})
	// The data is wanted before the picker is: the first time the pointer reaches
	// the emoji button, or the cursor reaches a composer, the files start to load.
	prefetch := func(event js.Value) {
		target := event.Get("target")
		if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
			return
		}
		if target.Call("closest", "button[data-action=emoji-toggle]").Truthy() || (target.Get("id").Type() == js.TypeString && (target.Get("id").String() == "chat-composer" || target.Get("id").String() == "thread-composer")) {
			chatEmojiEnsureData(false)
		}
	}
	add("pointerover", prefetch)
	add("focusin", prefetch)
	add("pointerover", func(event js.Value) {
		target := event.Get("target")
		if !inPicker(target) {
			return
		}
		button := target.Call("closest", ".emoji-pop-btn")
		if !button.Truthy() {
			return
		}
		if pos, ok := atoiOK(button.Get("dataset").Get("pos").String()); ok {
			chatEmojiHover(pos + 1)
		}
	})
	add("pointerout", func(event js.Value) {
		target := event.Get("target")
		if !matches(target, ".emoji-pop-btn") {
			return
		}
		// Moving between neighbouring emoji crosses a gap: stay on the last one until
		// the pointer leaves the grid altogether.
		if next := event.Get("relatedTarget"); !next.Truthy() || next.Get("closest").Type() != js.TypeFunction || !next.Call("closest", ".emoji-pop-grid").Truthy() {
			chatEmojiHover(0)
		}
	})
	// A press on a picker button must not take the cursor out of the search field.
	add("mousedown", func(event js.Value) {
		target := event.Get("target")
		if inPicker(target) && target.Call("closest", "button").Truthy() {
			event.Call("preventDefault")
		}
	})
	// A reaction chosen anywhere counts as a use: the picker, the message bar.
	add("click", func(event js.Value) {
		target := event.Get("target")
		if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
			return
		}
		// The emoji button opens its picker from the click itself.
		if opener := target.Call("closest", "button[data-action=emoji-toggle]"); opener.Truthy() && !opener.Get("disabled").Truthy() {
			id := opener.Get("dataset").Get("id").String()
			if st, open := chatEmojiHost.current(); !open || st.Reaction || st.Target != id {
				chatEmojiShowShell(id)
			}
		}
		// A click anywhere else on the page (another surface, Saved, details, a
		// dialog, nothing) closes the composer's picker. A target that a render has
		// already removed proves nothing, so it is not taken for "outside".
		if st, open := chatEmojiHost.current(); open && !st.Reaction && target.Get("isConnected").Truthy() && !inPicker(target) && !target.Call("closest", "button[data-action=emoji-toggle]").Truthy() {
			chatEmojiClose(false)
		}
		if button := target.Call("closest", "button[data-action=react-with]"); button.Truthy() && !button.Get("disabled").Truthy() {
			if glyph := button.Get("dataset").Get("emoji"); glyph.Type() == js.TypeString {
				chatEmojiHost.record(glyph.String())
			}
		}
	})
	add("keydown", func(event js.Value) {
		if event.Get("isComposing").Truthy() {
			return
		}
		if _, open := chatEmojiHost.current(); !open {
			return
		}
		target := event.Get("target")
		inside := inPicker(target)
		body := emojiDocument().Get("body")
		// Typing belongs to the picker while the cursor is in it, nowhere, or on the
		// button that opened it (a click leaves the cursor there); a key typed in
		// some other field is that field's.
		onOpener := matches(target, "button[data-action=emoji-toggle],button[data-action=react-pick]")
		key := event.Get("key").String()
		// Escape closes an open picker wherever the cursor is (in the composer, say).
		if key != "Escape" && !inside && !onOpener && target.Truthy() && !target.Equal(body) && !target.Equal(emojiDocument().Get("documentElement")) {
			return
		}
		inInput := matches(target, ".emoji-pop-input")
		stop := func() {
			event.Call("preventDefault")
			event.Call("stopImmediatePropagation")
		}
		picker := chatEmojiPickerElement()
		field := chatEmojiSearchField(picker)
		rtl := chatLayerRoot().Truthy() && chatLayerRoot().Get("dir").String() == "rtl"
		switch {
		case key == "Escape":
			// Escape clears a query first and closes second.
			if chatEmojiEscape() {
				emojiSetField(field, "")
			}
			stop()
		case key == "Enter":
			// On a control of the picker's own, Enter is that control's.
			if inside && !inInput && matches(target, "button") {
				return
			}
			// On the opener, Enter is the button's until something has been typed.
			if onOpener && (!field.Truthy() || strings.TrimSpace(field.Get("value").String()) == "") {
				return
			}
			if chatEmojiKey(key, event.Get("shiftKey").Truthy(), inInput, rtl) {
				stop()
			}
		case key == "ArrowDown" || key == "ArrowUp" || key == "ArrowLeft" || key == "ArrowRight" || key == "PageDown" || key == "PageUp":
			if chatEmojiKey(key, false, inInput, rtl) {
				stop()
			}
		case key == "Backspace" && !inInput && !event.Get("ctrlKey").Truthy() && !event.Get("metaKey").Truthy():
			if field.Truthy() {
				runes := []rune(field.Get("value").String())
				if len(runes) > 0 {
					runes = runes[:len(runes)-1]
				}
				emojiSetField(field, string(runes))
				chatEmojiSetQuery(string(runes))
				stop()
			}
		case !inInput && len([]rune(key)) == 1 && key != " " && !event.Get("ctrlKey").Truthy() && !event.Get("metaKey").Truthy() && !event.Get("altKey").Truthy():
			// Typing anywhere goes to the search field.
			if field.Truthy() {
				value := field.Get("value").String() + key
				emojiSetField(field, value)
				chatEmojiSetQuery(value)
				stop()
			}
		}
	})
	rewind := func() {
		for _, b := range bindings {
			doc.Call("removeEventListener", b.name, b.fn, true)
			b.fn.Release()
		}
	}
	// Scroll events do not bubble; the capture listener above catches them. The
	// window's resize and scroll re-place an open picker like the other layers.
	place := js.FuncOf(func(js.Value, []js.Value) any { chatEmojiAfterRender(); return nil })
	js.Global().Call("addEventListener", "resize", place)
	// An open picker belongs to the page as it was drawn: a move to another
	// conversation (the address changed) or a page restored from the browser's
	// back/forward cache starts with no picker open.
	leave := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Get("type").String() == "pageshow" && !args[0].Get("persisted").Truthy() {
			return nil
		}
		chatEmojiClose(false)
		return nil
	})
	js.Global().Call("addEventListener", "hashchange", leave)
	js.Global().Call("addEventListener", "pageshow", leave)
	// Choices made in the last few seconds are written when the page is put away.
	hide := js.FuncOf(func(js.Value, []js.Value) any {
		if doc.Get("visibilityState").String() == "hidden" {
			chatEmojiHost.flushPrefs()
		}
		return nil
	})
	doc.Call("addEventListener", "visibilitychange", hide)
	return func() {
		doc.Call("removeEventListener", "visibilitychange", hide)
		hide.Release()
		rewind()
		js.Global().Call("removeEventListener", "resize", place)
		place.Release()
		js.Global().Call("removeEventListener", "hashchange", leave)
		js.Global().Call("removeEventListener", "pageshow", leave)
		leave.Release()
	}
}

func emojiSetPx(style js.Value, name string, value float64) {
	style.Call("setProperty", name, strconv.FormatFloat(value, 'f', 2, 64)+"px")
}

func emojiRect(el js.Value) chatLayerRect {
	if !el.Truthy() || el.Get("getBoundingClientRect").Type() != js.TypeFunction {
		return chatLayerRect{}
	}
	r := el.Call("getBoundingClientRect")
	return chatLayerRect{r.Get("left").Float(), r.Get("top").Float(), r.Get("right").Float(), r.Get("bottom").Float()}
}

// chatEmojiAfterRender runs after every render of the workspace. When a picker is
// in the page it makes sure the floating layer is shown, puts it where the opener
// is, sizes the spacers that stand in for the rows that are not drawn, applies a
// requested scroll, and gives the search field the cursor the first time.
func chatEmojiAfterRender() {
	chatEmojiSettleShells()
	picker := chatEmojiPickerElement()
	if !picker.Truthy() {
		return
	}
	st, open := chatEmojiHost.current()
	if !open {
		return
	}
	if picker.Get("showPopover").Type() == js.TypeFunction && !picker.Call("matches", ":popover-open").Bool() {
		picker.Call("showPopover")
		markChatLayerOrder(picker)
	}
	chatEmojiPlaceLayer(picker, st)

	spacers := picker.Call("querySelectorAll", ".emoji-pop-spacer")
	for i := 0; i < spacers.Get("length").Int(); i++ {
		spacer := spacers.Index(i)
		if h, ok := atoiOK(spacer.Get("dataset").Get("h").String()); ok {
			emojiSetPx(spacer.Get("style"), "height", float64(h))
		}
	}
	scroller := picker.Call("querySelector", "[data-emoji-scroll]")
	if scroller.Truthy() && st.ScrollSet {
		scroller.Set("scrollTop", st.Scroll)
		next := st
		next.ScrollSet = false
		chatEmojiHost.silent(func(box *localUI) { box.emoji = next })
	}
	if scroller.Truthy() && st.ViewH <= 0 {
		next := st
		next.ViewH = scroller.Get("clientHeight").Float()
		chatEmojiHost.silent(func(box *localUI) { box.emoji = next })
	}
	// The search field has the cursor the moment the picker opens. This runs after
	// the render that drew the picker, not after a focus or blur event or a timer.
	// The input also carries autofocus, which showPopover honours by itself. The
	// marker is set only once the field really holds the cursor, so a render that
	// could not give it (the element was not laid out yet) tries again.
	if marker := picker.Get("__emojiFocused"); !marker.Truthy() {
		if field := chatEmojiSearchField(picker); field.Truthy() {
			// A field drawn again by a later render starts from the typed query.
			if field.Get("value").String() != st.Query {
				field.Set("value", st.Query)
			}
			field.Call("focus", emojiFocusOptions())
			if active := emojiDocument().Get("activeElement"); active.Truthy() && active.Equal(field) {
				picker.Set("__emojiFocused", true)
			}
		}
	}
}

// chatEmojiShowShell opens a composer's picker from the click itself: the hidden
// element the page already holds (chatEmojiShell) is shown, placed above the
// button and given the cursor, in the same event, before the render that the
// click's state change causes and before the emoji data has arrived. That render
// then fills the same element in.
func chatEmojiShowShell(target string) {
	shell := emojiDocument().Call("querySelector", "[data-emoji-shell][data-target='"+target+"']")
	if !shell.Truthy() || shell.Get("showPopover").Type() != js.TypeFunction || shell.Call("matches", ":popover-open").Bool() {
		return
	}
	touch, sheet := chatEmojiEnvironment()
	shell.Call("setAttribute", "data-touch", strconv.FormatBool(touch || sheet))
	shell.Call("setAttribute", "data-sheet", strconv.FormatBool(sheet))
	shell.Call("showPopover")
	chatEmojiPlaceLayer(shell, emojiOpenState(target, false, touch, sheet))
	if field := chatEmojiSearchField(shell); field.Truthy() {
		field.Call("focus", emojiFocusOptions())
		if active := emojiDocument().Get("activeElement"); active.Truthy() && active.Equal(field) {
			shell.Set("__emojiFocused", true)
		}
	}
}

// chatEmojiSettleShells puts away a shell that was shown by chatEmojiShowShell
// once its picker is closed: hidden again, with nothing left in its search field.
func chatEmojiSettleShells() {
	shells := emojiDocument().Call("querySelectorAll", "[data-emoji-shell]")
	st, open := chatEmojiHost.current()
	for i := 0; i < shells.Get("length").Int(); i++ {
		shell := shells.Index(i)
		if !shell.Call("matches", ":popover-open").Bool() {
			continue
		}
		if open && !st.Reaction && shell.Get("dataset").Get("target").String() == st.Target {
			continue
		}
		shell.Call("hidePopover")
		shell.Set("__emojiFocused", false)
		if field := chatEmojiSearchField(shell); field.Truthy() {
			field.Set("value", "")
		}
	}
}

// chatEmojiPlaceLayer sets the layer's rectangle from its opener. At phone width
// the stylesheet makes it a sheet along the bottom of the screen instead.
func chatEmojiPlaceLayer(picker js.Value, st emojiPickerState) {
	if st.Sheet {
		return
	}
	root := chatLayerRoot()
	win := js.Global()
	vw, vh := win.Get("innerWidth").Float(), win.Get("innerHeight").Float()
	rtl := root.Truthy() && root.Get("dir").String() == "rtl"
	var anchor, bounds chatLayerRect
	if st.Reaction {
		opener := js.Undefined()
		if root.Truthy() {
			opener = currentChatLayerOpener(root, "reaction")
		}
		var live chatLayerRect
		if opener.Truthy() && opener.Get("isConnected").Truthy() {
			live = emojiRect(chatLayerAnchor(opener, true))
		}
		rect, ok := chatLayerAnchorRect(live, rememberedChatLayerRect(root, "reaction"))
		if !ok {
			return
		}
		anchor, bounds = rect, chatLayerRect{0, 0, vw, vh}
	} else {
		button := emojiDocument().Call("querySelector", "[data-action=emoji-toggle][data-id='"+st.Target+"']")
		if !button.Truthy() {
			return
		}
		anchor = emojiRect(button)
		if !chatLayerRectUsable(anchor) {
			return
		}
		bounds = emojiRect(button.Call("closest", ".chat-composer,.thread-composer"))
	}
	placement := chatEmojiPlace(anchor, bounds, vw, vh, rtl, st.Reaction)
	style := picker.Get("style")
	emojiSetPx(style, "left", placement.Left)
	emojiSetPx(style, "top", placement.Top)
	emojiSetPx(style, "width", placement.Width)
	emojiSetPx(style, "height", placement.Height)
	emojiSetPx(style, "max-height", placement.Height)
}
