//go:build js && wasm

package chatui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

const issue3867InertMark = "data-phone-panel-inert"

// issue3867Kind is the panel that owned the screen after the last sync.
var issue3867Kind string

// SyncPhonePanelModal applies issue3867Plan to the page: the part behind a
// full-screen panel becomes inert and focus enters the panel when it opens; on
// close the page is live again and focus returns to the opener.
func SyncPhonePanelModal() { syncPhonePanelModal() }

func syncPhonePanelModal() {
	doc := js.Global().Get("document")
	if !doc.Truthy() || doc.Get("querySelector").Type() != js.TypeFunction {
		return
	}
	root := doc.Call("querySelector", ".chat-workspace")
	if !root.Truthy() {
		issue3867Kind = ""
		return
	}
	panels := make([]issue3867Panel, 0, len(issue3867PanelSelectors))
	elements := map[string]js.Value{}
	for _, p := range issue3867PanelSelectors {
		el := doc.Call("querySelector", p.Selector)
		if !el.Truthy() {
			continue
		}
		width := 0.0
		visible := el.Call("getClientRects").Get("length").Int() > 0
		if visible {
			width = el.Call("getBoundingClientRect").Get("width").Float()
		}
		panels = append(panels, issue3867Panel{Kind: p.Kind, Visible: visible, Width: width})
		elements[p.Kind] = el
	}
	plan := issue3867PlanPanels(panels, js.Global().Get("innerWidth").Float())
	want := map[string]bool{}
	for _, selector := range plan.Inert {
		want[selector] = true
	}
	// Release what an earlier plan made inert and this one does not.
	marked := root.Call("querySelectorAll", "["+issue3867InertMark+"]")
	for i := 0; i < marked.Length(); i++ {
		el := marked.Index(i)
		keep := false
		for selector := range want {
			if el.Call("matches", selector).Bool() {
				keep = true
			}
		}
		if !keep {
			el.Set("inert", false)
			el.Call("removeAttribute", issue3867InertMark)
		}
	}
	for selector := range want {
		if el := root.Call("querySelector", selector); el.Truthy() {
			el.Set("inert", true)
			el.Call("setAttribute", issue3867InertMark, "")
		}
	}
	previous := issue3867Kind
	issue3867Kind = plan.Kind
	if plan.Kind != "" {
		panel := elements[plan.Kind]
		if plan.Kind != previous || !panel.Call("contains", doc.Get("activeElement")).Bool() && issue3867FocusLost(doc) {
			issue3867FocusPanel(doc, panel)
		}
		return
	}
	if previous != "" && issue3867FocusLost(doc) {
		// The opener was inert while the panel was open, so a focus call made when
		// the close was pressed did nothing; the page is live again now.
		switch previous {
		case "thread", "details":
			restoreChatLayerFocus(previous)
		}
	}
}

// issue3867FocusLost reports that focus is on nothing (the page body), which is
// where the browser leaves it when the control that held it became inert.
func issue3867FocusLost(doc js.Value) bool {
	active := doc.Get("activeElement")
	return !active.Truthy() || active.Equal(doc.Get("body")) || active.Equal(doc.Get("documentElement"))
}

func issue3867FocusPanel(doc, panel js.Value) {
	if panel.Call("contains", doc.Get("activeElement")).Bool() {
		return
	}
	options := js.ValueOf(map[string]any{"preventScroll": true})
	candidates := panel.Call("querySelectorAll", issue3867FocusSelector)
	for i := 0; i < candidates.Length(); i++ {
		if el := candidates.Index(i); mobileRailFocusable(el) {
			el.Call("focus", options)
			return
		}
	}
	if panel.Get("tabIndex").Int() < 0 {
		panel.Call("focus", options)
	} else {
		panel.Call("setAttribute", "tabindex", "-1")
		panel.Call("focus", options)
	}
}

// trapPhonePanelFocus keeps Tab inside a full-screen panel: from its last
// control it wraps to its first, and the reverse for Shift+Tab.
func trapPhonePanelFocus(event ui.Event) bool {
	if issue3867Kind == "" || event.JSValue().Get("key").String() != "Tab" {
		return false
	}
	doc := js.Global().Get("document")
	var panel js.Value
	for _, p := range issue3867PanelSelectors {
		if p.Kind == issue3867Kind {
			panel = doc.Call("querySelector", p.Selector)
		}
	}
	if !panel.Truthy() {
		return false
	}
	items := panel.Call("querySelectorAll", issue3867FocusSelector)
	var visible []js.Value
	for i := 0; i < items.Length(); i++ {
		if item := items.Index(i); mobileRailFocusable(item) {
			visible = append(visible, item)
		}
	}
	if len(visible) == 0 {
		return false
	}
	active := doc.Get("activeElement")
	first, last := visible[0], visible[len(visible)-1]
	if event.JSValue().Get("shiftKey").Bool() {
		if active.Equal(first) || !panel.Call("contains", active).Bool() {
			last.Call("focus")
			return true
		}
	} else if active.Equal(last) || !panel.Call("contains", active).Bool() {
		first.Call("focus")
		return true
	}
	return false
}
