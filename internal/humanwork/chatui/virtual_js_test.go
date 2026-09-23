//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

func TestChatVirtualFocusRestoresOnlyLostEditor(t *testing.T) {
	obj := js.Global().Get("Object")
	doc, list, row, field := obj.New(), obj.New(), obj.New(), obj.New()
	body := obj.New()
	body.Set("tagName", "BODY")
	doc.Set("activeElement", body)
	row.Set("dataset", js.ValueOf(map[string]any{"virtualRow": "post"}))
	field.Set("id", "edit-post")
	rows, fields := js.Global().Get("Array").New(), js.Global().Get("Array").New()
	rows.Call("push", row)
	fields.Call("push", field)
	rowQuery := js.FuncOf(func(js.Value, []js.Value) any { return fields })
	listQuery := js.FuncOf(func(js.Value, []js.Value) any { return rows })
	focusCalls, selectionCalls := 0, 0
	focus := js.FuncOf(func(js.Value, []js.Value) any { focusCalls++; doc.Set("activeElement", field); return nil })
	selectRange := js.FuncOf(func(_ js.Value, args []js.Value) any {
		selectionCalls++
		if len(args) != 2 || args[0].Int() != 4 || args[1].Int() != 9 {
			t.Fatal("caret range changed")
		}
		return nil
	})
	row.Set("querySelectorAll", rowQuery)
	list.Set("querySelectorAll", listQuery)
	field.Set("focus", focus)
	field.Set("setSelectionRange", selectRange)
	defer rowQuery.Release()
	defer listQuery.Release()
	defer focus.Release()
	defer selectRange.Release()
	restoreVirtualFocus(doc, list, virtualFocus{rowID: "post", elementID: "edit-post", start: 4, end: 9, hasSelection: true})
	if focusCalls != 1 || selectionCalls != 1 || !doc.Get("activeElement").Equal(field) {
		t.Fatal("lost edit focus/caret not restored")
	}
	button := obj.New()
	button.Set("tagName", "BUTTON")
	doc.Set("activeElement", button)
	restoreVirtualFocus(doc, list, virtualFocus{rowID: "post", elementID: "edit-post", start: 4, end: 9, hasSelection: true})
	if focusCalls != 1 || !doc.Get("activeElement").Equal(button) {
		t.Fatal("intentional focus movement was stolen")
	}
}

func TestChatVirtualBottomUsesActualDOMHeight(t *testing.T) {
	oldDoc := js.Global().Get("document")
	defer js.Global().Set("document", oldDoc)
	obj := js.Global().Get("Object")
	doc, list := obj.New(), obj.New()
	style := obj.New()
	spacer := obj.New()
	spacer.Set("style", style)
	list.Set("clientHeight", 555)
	list.Set("scrollHeight", 228755)
	list.Set("isConnected", true)
	list.Set("__chatNearBottom", true)
	actualTop := 0.0
	getTop := js.FuncOf(func(js.Value, []js.Value) any { return actualTop })
	setTop := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			v := args[0].Float()
			if v > 228200 {
				v = 228200
			}
			actualTop = v
		}
		return nil
	})
	obj.Call("defineProperty", list, "scrollTop", map[string]any{"get": getTop, "set": setTop})
	defer getTop.Release()
	defer setTop.Release()
	querySpacer := js.FuncOf(func(js.Value, []js.Value) any { return spacer })
	list.Set("querySelector", querySpacer)
	defer querySpacer.Release()
	rows := js.Global().Get("Array").New()
	queryRows := js.FuncOf(func(js.Value, []js.Value) any { return rows })
	list.Set("querySelectorAll", queryRows)
	defer queryRows.Release()
	classes := obj.New()
	toggle := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	classes.Set("toggle", toggle)
	parent := obj.New()
	parent.Set("classList", classes)
	list.Set("parentElement", parent)
	defer toggle.Release()
	getList := js.FuncOf(func(js.Value, []js.Value) any { return list })
	doc.Set("getElementById", getList)
	defer getList.Release()
	doc.Set("activeElement", js.Null())
	js.Global().Set("document", doc)
	cache := &virtualCache{room: "room", heights: map[string]float64{}}
	layout := virtualLayout{active: true, before: 220000, after: 0, desired: 228184}
	syncVirtualTimeline(Model{SelectedID: "room"}, nil, layout, cache, virtualPosition{room: "room", bottom: true}, virtualFocus{}, func(virtualPosition) {}, func() {})
	if actualTop != 228200 {
		t.Fatalf("bottom gap = %v, want 0", 228200-actualTop)
	}
	// A small upward user scroll must release bottom following even while the
	// old virtual position still says bottom and the reader is within 120px.
	setChatScrollAway(list)
	actualTop = 228160
	if nearBottom(list) {
		t.Fatal("small upward scroll rearmed bottom following")
	}
	list.Set("__chatNearBottom", false)
	layout.desired = 228184
	syncVirtualTimeline(Model{SelectedID: "room"}, nil, layout, cache, virtualPosition{room: "room", bottom: true}, virtualFocus{}, func(virtualPosition) {}, func() {})
	if actualTop != 228160 {
		t.Fatalf("mutation after small upward scroll pulled reader to %v", actualTop)
	}
	scrollToEnd(list)
	if actualTop != 228200 || !nearBottom(list) {
		t.Fatal("explicit jump did not restore actual bottom following")
	}
	setChatScrollAway(list)
	layout.desired = 1000
	syncVirtualTimeline(Model{SelectedID: "room"}, nil, layout, cache, virtualPosition{room: "room", bottom: false}, virtualFocus{}, func(virtualPosition) {}, func() {})
	if actualTop != 1000 {
		t.Fatalf("reader who scrolled away was pulled to %v", actualTop)
	}
}
