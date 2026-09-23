//go:build js && wasm

package chatui

import (
	"strconv"
	"sync"
	"syscall/js"
)

// Column widths travel as attributes (data-rail-width, data-details-width on
// .chat-workspace) because the page's policy forbids inline style attributes.
// This file is the browser half: it carries each attribute into the matching
// custom property through the CSSOM, which the policy allows, and it lets the
// reader drag the seam between columns.
//
// A drag writes the property directly while the pointer moves, so the layout
// follows the hand with no round trip through the application, and commits
// the final width once on release through paneResizeCommit, which the render
// points at the model's ResizeRail/ResizeDetails callbacks. The application
// then re-emits the attribute with the same number, and the observer's write
// is a no-op.

const (
	paneHandleClass  = "pane-handle"
	paneRailAttr     = "data-rail-width"
	paneDetailsAttr  = "data-details-width"
	paneDraggingAttr = "data-pane-dragging"
)

// paneResizeCommit persists a width the reader chose. The render assigns it
// on every pass so it always addresses the live callbacks.
var paneResizeCommit func(pane string, px int)

var paneResizeOnce sync.Once

// paneDrag is the drag in progress, if any.
var paneDrag struct {
	pane      string
	startX    float64
	startW    int
	rtl       bool
	workspace js.Value
	handle    js.Value
	pointer   js.Value
}

func installPaneResize() {
	paneResizeOnce.Do(func() {
		doc := js.Global().Get("document")
		if !doc.Truthy() {
			return
		}
		down := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			e := args[0]
			target := e.Get("target")
			if !target.Truthy() || !target.Get("closest").Truthy() {
				return nil
			}
			handle := target.Call("closest", "."+paneHandleClass)
			if !handle.Truthy() {
				return nil
			}
			workspace := handle.Call("closest", ".chat-workspace")
			if !workspace.Truthy() {
				return nil
			}
			pane := handle.Get("dataset").Get("pane").String()
			paneDrag.pane = pane
			paneDrag.startX = e.Get("clientX").Float()
			paneDrag.startW = paneWidth(workspace, pane)
			paneDrag.rtl = workspace.Get("dir").String() == "rtl"
			paneDrag.workspace = workspace
			paneDrag.handle = handle
			paneDrag.pointer = e.Get("pointerId")
			paneLog("down", pane, paneDrag.startW, int(paneDrag.startX))
			workspace.Call("setAttribute", paneDraggingAttr, pane)
			handle.Get("classList").Call("add", "dragging")
			func() {
				defer func() { _ = recover() }()
				handle.Call("setPointerCapture", paneDrag.pointer)
			}()
			e.Call("preventDefault")
			return nil
		})
		move := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 || paneDrag.pane == "" {
				return nil
			}
			dx := args[0].Get("clientX").Float() - paneDrag.startX
			if paneDrag.rtl {
				dx = -dx
			}
			if paneDrag.pane == "details" {
				dx = -dx
			}
			px := clampPane(paneDrag.pane, paneDrag.startW+int(dx))
			setPaneProperty(paneDrag.workspace, paneDrag.pane, px)
			paneLog("move", paneDrag.pane, px, int(dx))
			return nil
		})
		up := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if paneDrag.pane == "" {
				return nil
			}
			pane, workspace, handle := paneDrag.pane, paneDrag.workspace, paneDrag.handle
			px := paneWidth(workspace, pane)
			paneLog("up", pane, px, 0)
			paneDrag.pane = ""
			workspace.Call("removeAttribute", paneDraggingAttr)
			handle.Get("classList").Call("remove", "dragging")
			if paneResizeCommit != nil {
				paneResizeCommit(pane, px)
			}
			return nil
		})
		doc.Call("addEventListener", "pointerdown", down)
		doc.Call("addEventListener", "pointermove", move)
		doc.Call("addEventListener", "pointerup", up)
		doc.Call("addEventListener", "pointercancel", up)
		syncAllPaneProperties()
	})
}

// applyPaneAttribute carries one width attribute into its custom property.
// It leaves a drag alone: the hand, not a late render, owns the width then.
func applyPaneAttribute(el js.Value, attr string) {
	if !el.Truthy() || el.Get("getAttribute").IsUndefined() {
		return
	}
	pane := "rail"
	if attr == paneDetailsAttr {
		pane = "details"
	}
	if paneDrag.pane == pane {
		return
	}
	value := el.Call("getAttribute", attr)
	if value.Type() != js.TypeString {
		return
	}
	px, err := strconv.Atoi(value.String())
	if err != nil || px <= 0 {
		return
	}
	setPaneProperty(el, pane, px)
}

func syncAllPaneProperties() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	found := doc.Call("querySelectorAll", ".chat-workspace")
	for i := 0; i < found.Length(); i++ {
		applyPaneAttribute(found.Index(i), paneRailAttr)
		applyPaneAttribute(found.Index(i), paneDetailsAttr)
	}
}

func setPaneProperty(workspace js.Value, pane string, px int) {
	workspace.Get("style").Call("setProperty", paneProperty(pane), strconv.Itoa(px)+"px")
}

func paneProperty(pane string) string {
	if pane == "details" {
		return "--chat-details"
	}
	return "--chat-rail"
}

// paneWidth reads the width a pane currently has, from the property when it
// is set and from the rendered column otherwise.
func paneWidth(workspace js.Value, pane string) int {
	raw := workspace.Get("style").Call("getPropertyValue", paneProperty(pane)).String()
	if px, err := strconv.Atoi(trimPx(raw)); err == nil && px > 0 {
		return px
	}
	selector := ".chat-rail"
	if pane == "details" {
		selector = ".chat-side"
	}
	if el := workspace.Call("querySelector", selector); el.Truthy() {
		if w := int(el.Call("getBoundingClientRect").Get("width").Float()); w > 0 {
			return w
		}
	}
	if pane == "details" {
		return DetailsDefault
	}
	return RailDefault
}

func trimPx(s string) string {
	for len(s) > 0 && (s[0] == ' ') {
		s = s[1:]
	}
	if len(s) > 2 && s[len(s)-2:] == "px" {
		s = s[:len(s)-2]
	}
	return s
}

func clampPane(pane string, px int) int {
	if pane == "details" {
		return clampInt(px, DetailsMin, DetailsMax)
	}
	return clampInt(px, RailMin, RailMax)
}

func clampInt(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

// paneLog keeps the last drag events on window.__chatPaneLog for diagnosis.
func paneLog(event, pane string, px, extra int) {
	global := js.Global()
	log := global.Get("__chatPaneLog")
	if !log.Truthy() {
		log = global.Get("Array").New()
		global.Set("__chatPaneLog", log)
	}
	log.Call("push", event+" "+pane+" px="+strconv.Itoa(px)+" x="+strconv.Itoa(extra))
	if log.Length() > 40 {
		log.Call("shift")
	}
}
