//go:build js && wasm

package chatui

import "syscall/js"

// composerSelection reads a composer's text and caret. The caret is the
// selection end in UTF-16 units; a range selection reports no caret so typing
// over a selection never opens the suggestion list.
func composerSelection(id string) (string, int, bool) {
	field := js.Global().Get("document").Call("getElementById", id)
	if !field.Truthy() {
		return "", 0, false
	}
	value := field.Get("value")
	start, end := field.Get("selectionStart"), field.Get("selectionEnd")
	if value.Type() != js.TypeString || start.Type() != js.TypeNumber || end.Type() != js.TypeNumber || start.Int() != end.Int() {
		return "", 0, false
	}
	return value.String(), end.Int(), true
}

// positionMentionMenu anchors the mention popover under the caret instead of
// the composer's left edge (C-19): the caret can be anywhere in a long
// message, and the left-edge anchor put the list nowhere near what the
// reader was actually looking at.
//
// A textarea exposes no caret coordinates, so this measures them the
// standard way: an offscreen mirror element gets the field's own font,
// padding and width, is filled with the text up to the caret, and its last
// line's width is the caret's x offset within the field. The field's own
// bounding rect turns that into a page position, which becomes the popover's
// inset-inline-start (clamped so a caret near the right edge does not push
// the 440px-wide list off the composer).
func positionMentionMenu(fieldID string) {
	doc := js.Global().Get("document")
	field := doc.Call("getElementById", fieldID)
	menu := doc.Call("querySelector", ".mention-menu")
	if !field.Truthy() || !menu.Truthy() {
		return
	}
	value := field.Get("value")
	caretVal := field.Get("selectionStart")
	if value.Type() != js.TypeString || caretVal.Type() != js.TypeNumber {
		return
	}
	caret := caretVal.Int()
	text := value.String()
	units := []rune(text)
	if caret > len(units) {
		caret = len(units)
	}
	before := string(units[:caret])
	mirror := doc.Call("createElement", "div")
	style := mirror.Get("style")
	computed := js.Global().Call("getComputedStyle", field)
	for _, prop := range []string{"fontFamily", "fontSize", "fontWeight", "letterSpacing", "paddingLeft", "paddingRight", "paddingTop", "borderLeftWidth", "borderRightWidth", "boxSizing", "lineHeight", "textIndent", "wordSpacing"} {
		style.Set(prop, computed.Call("getPropertyValue", cssPropertyName(prop)))
	}
	style.Set("position", "absolute")
	style.Set("visibility", "hidden")
	style.Set("whiteSpace", "pre-wrap")
	style.Set("wordWrap", "break-word")
	style.Set("top", "0")
	style.Set("left", "-9999px")
	rect := field.Call("getBoundingClientRect")
	style.Set("width", itoa(int(rect.Get("width").Float()))+"px")
	mirror.Set("textContent", before)
	caretMark := doc.Call("createElement", "span")
	caretMark.Set("textContent", "​")
	mirror.Call("appendChild", caretMark)
	doc.Get("body").Call("appendChild", mirror)
	markRect := caretMark.Call("getBoundingClientRect")
	mirrorRect := mirror.Call("getBoundingClientRect")
	offsetX := markRect.Get("left").Float() - mirrorRect.Get("left").Float()
	doc.Get("body").Call("removeChild", mirror)
	fieldWidth := rect.Get("width").Float()
	menuWidth := menu.Call("getBoundingClientRect").Get("width").Float()
	if menuWidth <= 0 {
		menuWidth = 340
	}
	left := offsetX
	if left+menuWidth > fieldWidth {
		left = fieldWidth - menuWidth
	}
	if left < 0 {
		left = 0
	}
	menu.Get("style").Set("insetInlineStart", itoa(int(left))+"px")
}

// cssPropertyName maps a camelCase JS style property to the hyphenated CSS
// property name getComputedStyle.getPropertyValue expects.
func cssPropertyName(prop string) string {
	var out []byte
	for _, r := range prop {
		if r >= 'A' && r <= 'Z' {
			out = append(out, '-', byte(r-'A'+'a'))
			continue
		}
		out = append(out, byte(r))
	}
	return string(out)
}

// replaceComposerText writes a composer's new text, keeps the caret where the
// caller put it and reports the change through the field's own input event, so
// the draft callback and a later render agree with what the field shows.
func replaceComposerText(id, value string, caret int) {
	doc := js.Global().Get("document")
	field := doc.Call("getElementById", id)
	if !field.Truthy() || field.Get("disabled").Truthy() {
		return
	}
	field.Set("value", value)
	field.Call("dispatchEvent", js.Global().Get("Event").New("input", map[string]any{"bubbles": true}))
	if field = doc.Call("getElementById", id); !field.Truthy() {
		return
	}
	field.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	field.Call("setSelectionRange", caret, caret)
}
