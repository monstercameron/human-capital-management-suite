//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

func TestMobileRailModalMarksOnlyBackgroundInert(t *testing.T) {
	global := js.Global()
	oldDoc, oldMatch := global.Get("document"), global.Get("matchMedia")
	defer func() { global.Set("document", oldDoc); global.Set("matchMedia", oldMatch) }()
	match := js.FuncOf(func(js.Value, []js.Value) any { return js.ValueOf(map[string]any{"matches": true}) })
	defer match.Release()
	global.Set("matchMedia", match)
	role, modal := "navigation", ""
	set := js.FuncOf(func(_ js.Value, args []js.Value) any {
		switch args[0].String() {
		case "role":
			role = args[1].String()
		case "aria-modal":
			modal = args[1].String()
		}
		return nil
	})
	defer set.Release()
	remove := js.FuncOf(func(_ js.Value, args []js.Value) any { modal = ""; return nil })
	defer remove.Release()
	rail := js.ValueOf(map[string]any{"setAttribute": set, "removeAttribute": remove})
	main := js.ValueOf(map[string]any{"inert": false})
	side := js.ValueOf(map[string]any{"inert": false})
	skip := js.ValueOf(map[string]any{"inert": false})
	rootQuery := js.FuncOf(func(_ js.Value, args []js.Value) any {
		switch args[0].String() {
		case ".chat-rail":
			return rail
		case ".chat-main":
			return main
		case ".chat-side":
			return side
		case ".chat-skip":
			return skip
		}
		return js.Null()
	})
	defer rootQuery.Release()
	root := js.ValueOf(map[string]any{"querySelector": rootQuery})
	query := js.FuncOf(func(js.Value, []js.Value) any { return root })
	defer query.Release()
	global.Set("document", js.ValueOf(map[string]any{"querySelector": query}))
	setMobileRailModal(true)
	if role != "dialog" || modal != "true" || !main.Get("inert").Bool() || !side.Get("inert").Bool() || !skip.Get("inert").Bool() {
		t.Fatalf("open drawer: role=%q modal=%q main=%v side=%v skip=%v", role, modal, main.Get("inert"), side.Get("inert"), skip.Get("inert"))
	}
	setMobileRailModal(false)
	if role != "navigation" || modal != "" || main.Get("inert").Bool() || side.Get("inert").Bool() || skip.Get("inert").Bool() {
		t.Fatal("closing drawer failed to restore navigation and background")
	}
}

func TestMobileRailMovesFocusIntoDrawerAndRestoresOpener(t *testing.T) {
	global := js.Global()
	oldDoc, oldMatch := global.Get("document"), global.Get("matchMedia")
	oldFrame, oldAdd, oldRemove := global.Get("requestAnimationFrame"), global.Get("addEventListener"), global.Get("removeEventListener")
	defer func() {
		global.Set("document", oldDoc)
		global.Set("matchMedia", oldMatch)
		global.Set("requestAnimationFrame", oldFrame)
		global.Set("addEventListener", oldAdd)
		global.Set("removeEventListener", oldRemove)
		mobileRailOpen = false
		mobileRailTrigger = js.Undefined()
	}()
	match := js.FuncOf(func(js.Value, []js.Value) any { return js.ValueOf(map[string]any{"matches": true}) })
	defer match.Release()
	global.Set("matchMedia", match)
	noop := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	defer noop.Release()
	global.Set("addEventListener", noop)
	global.Set("removeEventListener", noop)
	frame := js.FuncOf(func(_ js.Value, args []js.Value) any { args[0].Invoke(); return nil })
	defer frame.Release()
	global.Set("requestAnimationFrame", frame)
	var doc js.Value
	openerFocus := js.FuncOf(func(js.Value, []js.Value) any { doc.Set("activeElement", mobileRailTrigger); return nil })
	defer openerFocus.Release()
	mobileRailTrigger = js.ValueOf(map[string]any{"isConnected": true, "focus": openerFocus})
	var search js.Value
	searchFocus := js.FuncOf(func(js.Value, []js.Value) any { doc.Set("activeElement", search); return nil })
	defer searchFocus.Release()
	rects := js.FuncOf(func(js.Value, []js.Value) any { return js.ValueOf([]any{map[string]any{}}) })
	defer rects.Release()
	search = js.ValueOf(map[string]any{"focus": searchFocus, "isConnected": true, "getClientRects": rects})
	contains := js.FuncOf(func(_ js.Value, args []js.Value) any { return args[0].Equal(search) })
	defer contains.Release()
	railQuery := js.FuncOf(func(js.Value, []js.Value) any { return search })
	defer railQuery.Release()
	rail := js.ValueOf(map[string]any{"contains": contains, "querySelector": railQuery, "setAttribute": noop, "removeAttribute": noop})
	rootQuery := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() == ".chat-rail" {
			return rail
		}
		return js.Null()
	})
	defer rootQuery.Release()
	root := js.ValueOf(map[string]any{"querySelector": rootQuery, "dataset": map[string]any{"sidebarOpen": "true"}})
	query := js.FuncOf(func(js.Value, []js.Value) any { return root })
	defer query.Release()
	doc = js.ValueOf(map[string]any{"querySelector": query, "activeElement": mobileRailTrigger})
	global.Set("document", doc)
	mobileRailOpen = false
	syncMobileRailFocus(true)
	if !doc.Get("activeElement").Equal(search) {
		t.Fatal("opening the mobile drawer did not focus search")
	}
	syncMobileRailFocus(false)
	if !doc.Get("activeElement").Equal(mobileRailTrigger) {
		t.Fatal("closing the mobile drawer did not restore the opener")
	}
}

func TestMobileRailFocusableExcludesControlsInsideClosedDetails(t *testing.T) {
	details := js.ValueOf(map[string]any{})
	rects := js.FuncOf(func(js.Value, []js.Value) any { return js.ValueOf([]any{map[string]any{}}) })
	defer rects.Release()
	closestInput := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() == "details:not([open])" {
			return details
		}
		return js.Null()
	})
	defer closestInput.Release()
	input := js.ValueOf(map[string]any{"tagName": "INPUT", "parentElement": details, "closest": closestInput, "getClientRects": rects})
	if mobileRailFocusable(input) {
		t.Fatal("input in closed details remained in the drawer tab order")
	}
	closestSummary := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() == "details:not([open])" {
			return details
		}
		return js.Null()
	})
	defer closestSummary.Release()
	summary := js.ValueOf(map[string]any{"tagName": "SUMMARY", "parentElement": details, "closest": closestSummary, "getClientRects": rects})
	if !mobileRailFocusable(summary) {
		t.Fatal("summary for closed details was removed from the drawer tab order")
	}
}
