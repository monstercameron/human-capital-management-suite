//go:build js && wasm

package productui

import (
	"strconv"
	"syscall/js"
)

const docsSuggestBoxID = "docs-editor-suggest-box"

// docsSuggestPane is the editor pane an event happened in: the source
// textarea, the formatted pane, or neither.
func docsSuggestPane(target js.Value) (js.Value, string) {
	if !target.Truthy() {
		return js.Value{}, ""
	}
	if source := docsEditorElement(docsEditorSourceID); source.Truthy() && source.Equal(target) {
		return source, "source"
	}
	if rich := docsEditorElement(docsEditorRichID); docsEditorWithin(rich, target) {
		return rich, "rich"
	}
	return js.Value{}, ""
}

// docsSuggestBefore is the text from the start of the caret's line (or text
// node, in the formatted pane) up to the caret. It only reads: detection
// runs on every keystroke and must not touch the formatted pane's DOM.
func docsSuggestBefore(pane js.Value, kind string) (string, bool) {
	if kind == "source" {
		if pane.Get("selectionStart").Int() != pane.Get("selectionEnd").Int() {
			return "", false
		}
		value := pane.Get("value").String()
		before := value[:docsEditorUTF16ToByte(value, pane.Get("selectionStart").Int())]
		for i := len(before) - 1; i >= 0; i-- {
			if before[i] == '\n' {
				return before[i+1:], true
			}
		}
		return before, true
	}
	selection := js.Global().Call("getSelection")
	if !selection.Truthy() || selection.Get("rangeCount").Int() == 0 || !selection.Get("isCollapsed").Bool() {
		return "", false
	}
	node := selection.Get("anchorNode")
	if !node.Truthy() || node.Get("nodeType").Int() != 3 || !docsEditorWithin(pane, node) {
		return "", false
	}
	text := node.Get("data").String()
	return text[:docsEditorUTF16ToByte(text, selection.Get("anchorOffset").Int())], true
}

// docsSuggestCaretRect is the caret's viewport rectangle. A textarea has no
// range for its caret, so a hidden mirror with the same box and font lays
// out the text before the caret and reports where it ends.
func docsSuggestCaretRect(pane js.Value, kind string) (left, top, bottom float64, ok bool) {
	if kind == "rich" {
		selection := js.Global().Call("getSelection")
		if !selection.Truthy() || selection.Get("rangeCount").Int() == 0 {
			return 0, 0, 0, false
		}
		r := selection.Call("getRangeAt", 0).Call("cloneRange")
		var rect js.Value
		if rects := r.Call("getClientRects"); rects.Get("length").Int() > 0 {
			rect = rects.Index(0)
		} else {
			node := r.Get("startContainer")
			if node.Get("nodeType").Int() == 3 {
				node = node.Get("parentElement")
			}
			rect = node.Call("getBoundingClientRect")
		}
		return rect.Get("left").Float(), rect.Get("top").Float(), rect.Get("bottom").Float(), true
	}
	doc := js.Global().Get("document")
	computed := js.Global().Call("getComputedStyle", pane)
	mirror := doc.Call("createElement", "div")
	style := mirror.Get("style")
	for _, name := range []string{"box-sizing", "width", "padding-top", "padding-right", "padding-bottom", "padding-left", "border-top-width", "border-right-width", "border-bottom-width", "border-left-width", "border-style", "font-family", "font-size", "font-weight", "font-style", "line-height", "letter-spacing", "word-spacing", "tab-size", "text-indent", "direction"} {
		style.Call("setProperty", name, computed.Call("getPropertyValue", name))
	}
	box := pane.Call("getBoundingClientRect")
	style.Call("setProperty", "position", "fixed")
	style.Call("setProperty", "visibility", "hidden")
	style.Call("setProperty", "white-space", "pre-wrap")
	style.Call("setProperty", "overflow-wrap", "break-word")
	style.Call("setProperty", "left", docsSuggestPx(box.Get("left").Float()))
	style.Call("setProperty", "top", docsSuggestPx(box.Get("top").Float()))
	value := pane.Get("value").String()
	mirror.Set("textContent", value[:docsEditorUTF16ToByte(value, pane.Get("selectionStart").Int())])
	marker := doc.Call("createElement", "span")
	marker.Set("textContent", "|")
	mirror.Call("appendChild", marker)
	doc.Get("body").Call("appendChild", mirror)
	rect := marker.Call("getBoundingClientRect")
	scroll := pane.Get("scrollTop").Float()
	left, top, bottom = rect.Get("left").Float()-pane.Get("scrollLeft").Float(), rect.Get("top").Float()-scroll, rect.Get("bottom").Float()-scroll
	mirror.Call("remove")
	return left, top, bottom, true
}

func docsSuggestPx(value float64) string { return strconv.FormatFloat(value, 'f', 1, 64) + "px" }

// docsSuggestMount follows typing in either pane, routes the list's keys
// before the editor sees them (capture phase, so Escape closes the list
// and not the editor) and keeps the list beside the caret, placed through
// the CSSOM because the product's CSP allows no style attributes.
func docsSuggestMount(c *docsEditorController, s *docsSuggestController) func() {
	if c == nil || s == nil {
		return func() {}
	}
	doc := js.Global().Get("document")
	var pane js.Value
	paneKind := ""
	var listeners []docsEditorListener
	listen := func(name string, capture bool, handler func(js.Value)) {
		fn := js.FuncOf(func(_ js.Value, args []js.Value) any {
			defer docsEditorContain()
			if len(args) > 0 {
				handler(args[0])
			}
			return nil
		})
		doc.Call("addEventListener", name, fn, capture)
		listeners = append(listeners, docsEditorListener{name: name, fn: fn, capture: capture})
	}
	aria := func() {
		for _, id := range []string{docsEditorSourceID, docsEditorRichID} {
			el := docsEditorElement(id)
			if !el.Truthy() {
				continue
			}
			el.Call("setAttribute", "aria-autocomplete", "list")
			el.Call("setAttribute", "aria-controls", "docs-editor-suggest")
			open := s.state.Open && pane.Truthy() && el.Equal(pane)
			el.Call("setAttribute", "aria-expanded", strconv.FormatBool(open))
			if open && len(s.state.Items) > 0 {
				el.Call("setAttribute", "aria-activedescendant", docsSuggestOptionID(s.state.Active))
			} else {
				el.Call("removeAttribute", "aria-activedescendant")
			}
		}
	}
	position := func() {
		box := docsEditorElement(docsSuggestBoxID)
		if !box.Truthy() || !pane.Truthy() {
			return
		}
		left, top, bottom, ok := docsSuggestCaretRect(pane, paneKind)
		if !ok {
			return
		}
		win := js.Global()
		width, height := win.Get("innerWidth").Float(), win.Get("innerHeight").Float()
		// Below the caret, or above it when the list would run off the
		// bottom; the inline start follows the caret in either direction.
		place := bottom + 4
		if listHeight := box.Get("offsetHeight").Float(); listHeight > 0 && place+listHeight > height-8 && top-listHeight-4 > 8 {
			place = top - listHeight - 4
		}
		start := left
		if win.Call("getComputedStyle", box).Get("direction").String() == "rtl" {
			start = width - left
		}
		if limit := width - box.Get("offsetWidth").Float() - 8; start > limit {
			start = limit
		}
		if start < 8 {
			start = 8
		}
		style := box.Get("style")
		style.Call("setProperty", "--docs-suggest-top", docsSuggestPx(place))
		style.Call("setProperty", "--docs-suggest-start", docsSuggestPx(start))
	}
	// One frame callback per editor mount. It is never released: a frame
	// requested just before unmount may still call it, and a released
	// function would end the program.
	frame := js.FuncOf(func(js.Value, []js.Value) any {
		defer docsEditorContain()
		position()
		return nil
	})
	s.onPosition = func() {
		aria()
		js.Global().Call("requestAnimationFrame", frame)
	}
	s.onPick = func(item DocsReferenceSuggestion, kind string) {
		capture := docsEditorCaptureSelection(c)
		if capture.Start == capture.End {
			if next, at, ok := docsSuggestApply(capture.Markdown, capture.Start, kind, item.Insert); ok {
				docsEditorCommit(c, next, at, at, capture.Pane)
			}
		}
		aria()
	}
	detect := func(target js.Value) {
		el, kind := docsSuggestPane(target)
		if kind == "" {
			s.close()
			aria()
			return
		}
		pane, paneKind = el, kind
		before, ok := docsSuggestBefore(el, kind)
		trigger, query, _, found := docsSuggestTrigger(before)
		s.update(trigger, query, ok && found)
		aria()
	}
	listen("input", false, func(event js.Value) { detect(event.Get("target")) })
	listen("keyup", false, func(event js.Value) {
		switch event.Get("key").String() {
		case "ArrowLeft", "ArrowRight", "Home", "End":
			detect(event.Get("target"))
		}
	})
	listen("mouseup", false, func(event js.Value) {
		if _, kind := docsSuggestPane(event.Get("target")); kind != "" {
			detect(event.Get("target"))
		}
	})
	listen("keydown", true, func(event js.Value) {
		if !s.state.Open || event.Get("isComposing").Bool() {
			return
		}
		if _, kind := docsSuggestPane(event.Get("target")); kind == "" {
			return
		}
		if s.key(event.Get("key").String()) {
			event.Call("preventDefault")
			event.Call("stopPropagation")
			aria()
		}
	})
	listen("focusin", false, func(event js.Value) {
		if _, kind := docsSuggestPane(event.Get("target")); kind == "" {
			s.close()
			aria()
		}
	})
	listen("scroll", true, func(js.Value) {
		if s.state.Open {
			position()
		}
	})
	return func() {
		for _, l := range listeners {
			doc.Call("removeEventListener", l.name, l.fn, l.capture)
			l.fn.Release()
		}
		s.onPosition, s.onPick = nil, nil
		s.close()
	}
}
