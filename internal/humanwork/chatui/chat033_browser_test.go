//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

// paneStyleStub is a minimal CSSOM style object: real setProperty/
// getPropertyValue semantics (missing property reads back "", not
// undefined), which is what the pane-resize CSSOM code depends on.
func paneStyleStub() js.Value {
	style := js.Global().Get("Object").New()
	props := js.Global().Get("Object").New()
	style.Set("setProperty", js.FuncOf(func(_ js.Value, args []js.Value) any {
		props.Set(args[0].String(), args[1])
		return nil
	}))
	style.Set("getPropertyValue", js.FuncOf(func(_ js.Value, args []js.Value) any {
		v := props.Get(args[0].String())
		if v.Type() != js.TypeString {
			return ""
		}
		return v
	}))
	return style
}

// paneElementStub is a minimal DOM element stub with attribute storage.
func paneElementStub() js.Value {
	el := js.Global().Get("Object").New()
	attrs := js.Global().Get("Object").New()
	el.Set("setAttribute", js.FuncOf(func(_ js.Value, args []js.Value) any { attrs.Set(args[0].String(), args[1]); return nil }))
	el.Set("removeAttribute", js.FuncOf(func(_ js.Value, args []js.Value) any { attrs.Delete(args[0].String()); return nil }))
	el.Set("getAttribute", js.FuncOf(func(_ js.Value, args []js.Value) any {
		v := attrs.Get(args[0].String())
		if v.Type() != js.TypeString {
			return js.Null()
		}
		return v
	}))
	return el
}

// TestTodo_CHAT_033_Browser is the CHAT-033 BROWSER matrix test. It exercises
// the browser-side pane-resize CSSOM code directly: reading a pane's current
// width falls back to the rendered column before a drag, a dragged width is
// bounded per pane and owned by the CSS custom property, a server-rendered
// attribute hydrates into the same property on mount, and releasing a drag
// persists through the exact commit function the render wires to the
// model's resize callbacks.
func TestTodo_CHAT_033_Browser(t *testing.T) {
	// Before any drag, the width comes from the rendered column's box.
	workspace := paneElementStub()
	workspace.Set("style", paneStyleStub())
	rail := paneElementStub()
	railRect := js.Global().Get("Object").New()
	railRect.Set("width", 240)
	rail.Set("getBoundingClientRect", js.FuncOf(func(js.Value, []js.Value) any { return railRect }))
	workspace.Set("querySelector", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() == ".chat-rail" {
			return rail
		}
		return js.Null()
	}))
	if got := paneWidth(workspace, "rail"); got != 240 {
		t.Fatalf("pre-drag rail width = %d, want the rendered column's box (240)", got)
	}

	// A drag writes the CSS custom property directly, which then owns the
	// width, bounded per device-class range even for an out-of-range delta.
	setPaneProperty(workspace, "rail", clampPane("rail", 5000))
	if got := paneWidth(workspace, "rail"); got != RailMax {
		t.Fatalf("dragged rail width = %d, want clamped to RailMax=%d", got, RailMax)
	}
	setPaneProperty(workspace, "rail", clampPane("rail", 10))
	if got := paneWidth(workspace, "rail"); got != RailMin {
		t.Fatalf("dragged rail width = %d, want clamped to RailMin=%d", got, RailMin)
	}
	setPaneProperty(workspace, "details", clampPane("details", 5000))
	if got := paneWidth(workspace, "details"); got != DetailsMax {
		t.Fatalf("dragged details width = %d, want clamped to DetailsMax=%d", got, DetailsMax)
	}

	// A server-rendered attribute (the SSR path's own record of the last
	// committed width) hydrates into the same CSSOM property on mount.
	hydrated := paneElementStub()
	hydrated.Set("style", paneStyleStub())
	hydrated.Call("setAttribute", paneRailAttr, "360")
	applyPaneAttribute(hydrated, paneRailAttr)
	if got := paneWidth(hydrated, "rail"); got != 360 {
		t.Fatalf("hydrated rail width = %d, want 360 from the server attribute", got)
	}
	// Hydration never overrides a live drag on the same pane.
	paneDrag.pane = "rail"
	hydrated.Call("setAttribute", paneRailAttr, "999")
	applyPaneAttribute(hydrated, paneRailAttr)
	paneDrag.pane = ""
	if got := paneWidth(hydrated, "rail"); got != 360 {
		t.Fatalf("hydration clobbered an in-progress drag: rail width = %d, want 360", got)
	}

	// Releasing a drag persists through the exact commit function the render
	// wires to the model's ResizeRail/ResizeDetails callbacks (see
	// paneResizeCommit's assignment in render.go's Workspace): this is the
	// browser half of "bounded pane sizes... persist per device class."
	var gotRail, gotDetails int
	railCalls, detailsCalls := 0, 0
	callbacks := Callbacks{
		ResizeRail:    func(px int) { gotRail = px; railCalls++ },
		ResizeDetails: func(px int) { gotDetails = px; detailsCalls++ },
	}
	commit := func(pane string, px int) {
		switch pane {
		case "details":
			if callbacks.ResizeDetails != nil {
				callbacks.ResizeDetails(px)
			}
		default:
			if callbacks.ResizeRail != nil {
				callbacks.ResizeRail(px)
			}
		}
	}
	previous := paneResizeCommit
	paneResizeCommit = commit
	defer func() { paneResizeCommit = previous }()
	paneResizeCommit("rail", clampPane("rail", 316))
	paneResizeCommit("details", clampPane("details", 344))
	if gotRail != 316 || railCalls != 1 {
		t.Fatalf("rail commit = %d (calls=%d), want 316 once", gotRail, railCalls)
	}
	if gotDetails != 344 || detailsCalls != 1 {
		t.Fatalf("details commit = %d (calls=%d), want 344 once", gotDetails, detailsCalls)
	}
}
