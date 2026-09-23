//go:build js && wasm

package chatui

import (
	"math"
	"strconv"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func virtualScrollGeometry(event ui.Event) (float64, float64) {
	target := event.JSValue().Get("currentTarget")
	if !target.Truthy() {
		return 0, 0
	}
	return target.Get("scrollTop").Float(), target.Get("clientHeight").Float()
}

func virtualAwayIntent(event ui.Event) bool {
	return event.JSValue().Get("currentTarget").Get("__chatScrollAwayIntent").Truthy()
}

func virtualBottomDisarmed(room string) bool {
	doc := js.Global().Get("document")
	if !doc.Truthy() || doc.Get("getElementById").Type() != js.TypeFunction {
		return false
	}
	list := doc.Call("getElementById", "chat-main")
	return list.Truthy() && chatAnchorRoom(list.Call("getAttribute", listAnchorAttr).String()) == room && list.Get("__chatScrollAwayIntent").Truthy()
}

func focusedVirtualMessageID() string {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return ""
	}
	active := doc.Get("activeElement")
	if !active.Truthy() || !active.Get("closest").Truthy() {
		return ""
	}
	row := active.Call("closest", "[data-virtual-row]")
	if !row.Truthy() {
		return ""
	}
	return row.Get("dataset").Get("virtualRow").String()
}

func captureVirtualFocus() virtualFocus {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return virtualFocus{}
	}
	active := doc.Get("activeElement")
	if !active.Truthy() || !active.Get("closest").Truthy() {
		return virtualFocus{}
	}
	row := active.Call("closest", "[data-virtual-row]")
	if !row.Truthy() {
		return virtualFocus{}
	}
	result := virtualFocus{rowID: row.Get("dataset").Get("virtualRow").String(), elementID: active.Get("id").String()}
	start, end := active.Get("selectionStart"), active.Get("selectionEnd")
	if start.Type() == js.TypeNumber && end.Type() == js.TypeNumber {
		result.start, result.end, result.hasSelection = start.Int(), end.Int(), true
	}
	return result
}

func imageViewerPostID() string { return imageViewerPost }

func syncVirtualTimeline(m Model, messages []Message, layout virtualLayout, cache *virtualCache, pos virtualPosition, focus virtualFocus, setPosition func(virtualPosition), invalidate func()) func() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return nil
	}
	list := js.Null()
	if doc.Get("getElementById").Type() == js.TypeFunction {
		list = doc.Call("getElementById", "chat-main")
	} else if doc.Get("querySelector").Type() == js.TypeFunction {
		list = doc.Call("querySelector", "#chat-main")
	}
	if !list.Truthy() {
		return nil
	}
	if layout.active {
		setSpacer := func(which string, height float64) {
			spacer := list.Call("querySelector", "[data-virtual-spacer='"+which+"']")
			if spacer.Truthy() {
				spacer.Get("style").Set("height", strconv.FormatFloat(math.Max(0, height), 'f', 1, 64)+"px")
			}
		}
		setSpacer("before", layout.before)
		setSpacer("after", layout.after)
		if (pos.room != m.SelectedID || !list.Get("__chatScrollAwayIntent").Truthy() || !pos.bottom || jumpPendingForRoom(m.SelectedID)) && (pos.room != m.SelectedID || math.Abs(list.Get("scrollTop").Float()-layout.desired) > 2) {
			list.Set("scrollTop", layout.desired)
		}
		if jumpPendingForRoom(m.SelectedID) || pos.room != m.SelectedID || (pos.bottom && list.Get("__chatNearBottom").Truthy()) {
			scrollToEnd(list)
		}
	}
	restoreVirtualFocus(doc, list, focus)
	ids := visibleMessageIDs(m, messages, layout)
	if signature := visibleMessageSignature(ids); signature != cache.lastIDs {
		cache.lastIDs = signature
		if m.Callbacks.VisibleMessageIDs != nil {
			m.Callbacks.VisibleMessageIDs(append([]string(nil), ids...))
		}
	}
	keep := make(map[string]bool, len(messages))
	for _, msg := range messages {
		keep[msg.ID] = true
	}
	for id := range cache.heights {
		if !keep[id] {
			delete(cache.heights, id)
		}
	}
	if !layout.active {
		return nil
	}
	observerType := js.Global().Get("ResizeObserver")
	if !observerType.Truthy() {
		return nil
	}
	callback := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || !list.Get("isConnected").Truthy() {
			return nil
		}
		changed := false
		entries := args[0]
		for i := 0; i < entries.Length(); i++ {
			entry := entries.Index(i)
			target := entry.Get("target")
			if target.Equal(list) {
				h := list.Get("clientHeight").Float()
				if h > 0 && math.Abs(h-pos.height) > 2 {
					next := pos
					next.height = h
					setPosition(next)
				}
				continue
			}
			if target.Get("hasAttribute").Truthy() && target.Call("hasAttribute", "data-virtual-prelude").Bool() {
				h := entry.Get("contentRect").Get("height").Float()
				if h > 0 && math.Abs(cache.prelude-h) > 2 {
					cache.prelude = h
					changed = true
				}
				continue
			}
			if target.Get("hasAttribute").Truthy() && target.Call("hasAttribute", "data-virtual-footer").Bool() {
				h := entry.Get("contentRect").Get("height").Float()
				if h > 0 && math.Abs(cache.footer-h) > 2 {
					cache.footer = h
					changed = true
				}
				continue
			}
			if target.Get("classList").Call("contains", "virtual-parked").Bool() {
				continue
			}
			id := target.Get("dataset").Get("virtualRow").String()
			h := entry.Get("contentRect").Get("height").Float()
			if id != "" && h > 0 && math.Abs(cache.heights[id]-h) > 2 {
				cache.heights[id] = h
				changed = true
			}
		}
		if changed {
			invalidate()
		}
		return nil
	})
	observer := observerType.New(callback)
	observer.Call("observe", list)
	if prelude := list.Call("querySelector", "[data-virtual-prelude]"); prelude.Truthy() {
		observer.Call("observe", prelude)
	}
	if footer := list.Call("querySelector", "[data-virtual-footer]"); footer.Truthy() {
		observer.Call("observe", footer)
	}
	rows := list.Call("querySelectorAll", "[data-virtual-row]:not(.virtual-parked)")
	for i := 0; i < rows.Length(); i++ {
		observer.Call("observe", rows.Index(i))
	}
	return func() { observer.Call("disconnect"); callback.Release() }
}

func restoreVirtualFocus(doc, list js.Value, focus virtualFocus) {
	if focus.rowID == "" || focus.elementID == "" {
		return
	}
	active := doc.Get("activeElement")
	if active.Truthy() && active.Get("tagName").String() != "BODY" {
		return
	}
	if list.Get("querySelectorAll").Type() != js.TypeFunction {
		return
	}
	rows := list.Call("querySelectorAll", "[data-virtual-row]")
	for i := 0; i < rows.Length(); i++ {
		row := rows.Index(i)
		if row.Get("dataset").Get("virtualRow").String() != focus.rowID || row.Get("querySelectorAll").Type() != js.TypeFunction {
			continue
		}
		fields := row.Call("querySelectorAll", "[id]")
		for j := 0; j < fields.Length(); j++ {
			field := fields.Index(j)
			if field.Get("id").String() != focus.elementID {
				continue
			}
			field.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
			if focus.hasSelection && field.Get("setSelectionRange").Type() == js.TypeFunction {
				field.Call("setSelectionRange", focus.start, focus.end)
			}
			return
		}
	}
}
