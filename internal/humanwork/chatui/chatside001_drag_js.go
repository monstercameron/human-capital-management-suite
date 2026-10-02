//go:build js && wasm

package chatui

import (
	"math"
	"sync"
	"syscall/js"
)

// CHATSIDE-001: the browser half of dragging a sidebar row onto a section. It
// listens for pointer events on the document, once, and tells the application
// where the row was dropped through chatside001Commit, which the render points
// at the model's MoveConversationSection each pass. A mouse or a pen drags; a
// touch scrolls the list and moves rows with the row menu instead.

const (
	chatside001DragSlop    = 6.0
	chatside001DropClass   = "side-drop"
	chatside001DraggingRow = "side-dragging"
	chatside001DragAttr    = "data-side-dragging"
)

var chatside001Commit func(string, string)
var chatside001Once sync.Once

type chatside001DragState struct {
	id, kind       string
	startX, startY float64
	pointer        js.Value
	row, origin    js.Value
	workspace      js.Value
	target         js.Value
	active         bool
	suppressClick  bool
}

var chatside001State chatside001DragState

func chatside001InstallDrag(commit func(string, string)) {
	chatside001Commit = commit
	chatside001Once.Do(func() {
		doc := js.Global().Get("document")
		if !doc.Truthy() {
			return
		}
		reset := func() {
			state := &chatside001State
			if state.target.Truthy() {
				state.target.Get("classList").Call("remove", chatside001DropClass)
			}
			if state.row.Truthy() {
				state.row.Get("classList").Call("remove", chatside001DraggingRow)
			}
			if state.workspace.Truthy() {
				state.workspace.Call("removeAttribute", chatside001DragAttr)
			}
			*state = chatside001DragState{suppressClick: state.suppressClick}
		}
		down := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			e := args[0]
			if drag, _ := chatside001Gesture(e.Get("pointerType").String()); e.Get("button").Int() != 0 || !drag {
				return nil
			}
			target := e.Get("target")
			if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction || target.Call("closest", ".rail-row-more").Truthy() {
				return nil
			}
			row := target.Call("closest", ".chat-rail-row[data-conversation-id]")
			if !row.Truthy() {
				return nil
			}
			reset()
			state := &chatside001State
			state.id = row.Get("dataset").Get("conversationId").String()
			state.kind = row.Get("dataset").Get("sideKind").String()
			state.startX, state.startY = e.Get("clientX").Float(), e.Get("clientY").Float()
			state.pointer, state.row = e.Get("pointerId"), row
			state.origin = row.Call("closest", ".sidebar-section")
			state.workspace = row.Call("closest", ".chat-workspace")
			return nil
		})
		move := js.FuncOf(func(_ js.Value, args []js.Value) any {
			state := &chatside001State
			if len(args) == 0 || state.id == "" || args[0].Get("pointerId").String() != state.pointer.String() {
				return nil
			}
			e := args[0]
			x, y := e.Get("clientX").Float(), e.Get("clientY").Float()
			if !state.active {
				if math.Hypot(x-state.startX, y-state.startY) < chatside001DragSlop {
					return nil
				}
				state.active = true
				state.row.Get("classList").Call("add", chatside001DraggingRow)
				if state.workspace.Truthy() {
					state.workspace.Call("setAttribute", chatside001DragAttr, "true")
				}
			}
			var next js.Value
			if at := js.Global().Get("document").Call("elementFromPoint", x, y); at.Truthy() {
				if section := at.Call("closest", ".sidebar-section"); section.Truthy() && !section.Equal(state.origin) {
					accepts := section.Get("dataset").Get("sideAccepts").String()
					if accepts == "any" || accepts == state.kind {
						next = section
					}
				}
			}
			if state.target.Truthy() && !(next.Truthy() && next.Equal(state.target)) {
				state.target.Get("classList").Call("remove", chatside001DropClass)
			}
			if next.Truthy() {
				next.Get("classList").Call("add", chatside001DropClass)
			}
			state.target = next
			return nil
		})
		up := js.FuncOf(func(_ js.Value, args []js.Value) any {
			state := &chatside001State
			if len(args) == 0 || state.id == "" || args[0].Get("pointerId").String() != state.pointer.String() {
				return nil
			}
			id, sectionID, dragged := state.id, "", state.active
			if state.target.Truthy() {
				sectionID = state.target.Get("dataset").Get("sectionId").String()
			}
			reset()
			if dragged {
				// The release must not also press the row it began on.
				chatside001State.suppressClick = true
				js.Global().Call("setTimeout", js.FuncOf(func(js.Value, []js.Value) any {
					chatside001State.suppressClick = false
					return nil
				}), 0)
			}
			if sectionID != "" && chatside001Commit != nil {
				chatside001Commit(id, sectionID)
			}
			return nil
		})
		cancel := js.FuncOf(func(js.Value, []js.Value) any {
			reset()
			return nil
		})
		swallow := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if chatside001State.suppressClick && len(args) > 0 {
				args[0].Call("stopPropagation")
				args[0].Call("preventDefault")
			}
			return nil
		})
		doc.Call("addEventListener", "pointerdown", down)
		doc.Call("addEventListener", "pointermove", move)
		doc.Call("addEventListener", "pointerup", up)
		doc.Call("addEventListener", "pointercancel", cancel)
		doc.Call("addEventListener", "click", swallow, true)
	})
}
