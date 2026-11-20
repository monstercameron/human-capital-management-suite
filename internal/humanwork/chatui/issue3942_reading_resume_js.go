//go:build js && wasm

package chatui

import "syscall/js"

// ReadingPosition is the message at the top of the timeline and how many pixels
// its top edge sits below the list's top edge (negative when it is partly
// scrolled out). ok is false when nothing is drawn or the reader is at the
// newest messages, where there is no place to come back to.
func ReadingPosition() (id string, offset int, ok bool) {
	defer func() {
		if recover() != nil {
			id, offset, ok = "", 0, false
		}
	}()
	list := js.Global().Get("document").Call("querySelector", "["+listAnchorAttr+"]")
	if !list.Truthy() || nearBottom(list) {
		return "", 0, false
	}
	top := list.Call("getBoundingClientRect").Get("top").Float()
	rows := list.Call("querySelectorAll", "[data-message-id]")
	for i := 0; i < rows.Length(); i++ {
		row := rows.Index(i)
		rect := row.Call("getBoundingClientRect")
		if rect.Get("bottom").Float() > top+1 {
			return row.Get("dataset").Get("messageId").String(), int(rect.Get("top").Float() - top), true
		}
	}
	return "", 0, false
}

// RestoreReadingPosition waits for the message's row to be drawn, then scrolls
// the list so the row sits at the offset it was read at, and keeps it there for
// a short settle window while images and late rows change heights. The reader
// scrolling, touching or pressing a key ends the watch at once. missing runs
// once if the row never appears.
func RestoreReadingPosition(id string, offset int, missing func()) {
	if id == "" {
		return
	}
	global := js.Global()
	if !global.Get("requestAnimationFrame").Truthy() {
		return
	}
	watch := &readingWatch{id: id, offset: float64(offset), missing: missing}
	watch.doc = global.Get("document")
	watch.stopInput = js.FuncOf(func(js.Value, []js.Value) any {
		watch.done = true
		return nil
	})
	watch.events = []string{"wheel", "touchstart", "keydown", "pointerdown"}
	for _, name := range watch.events {
		watch.doc.Call("addEventListener", name, watch.stopInput, js.ValueOf(map[string]any{"passive": true, "capture": true}))
	}
	watch.tick = js.FuncOf(func(js.Value, []js.Value) any {
		watch.step()
		return nil
	})
	if !watch.schedule() {
		watch.finish()
	}
}

type readingWatch struct {
	id        string
	offset    float64
	missing   func()
	tick      js.Func
	stopInput js.Func
	doc       js.Value
	events    []string
	settled   int
	absent    int
	placed    bool
	done      bool
}

func (w *readingWatch) schedule() (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	js.Global().Call("requestAnimationFrame", w.tick)
	return true
}

func (w *readingWatch) finish() {
	if w.stopInput.Truthy() {
		for _, name := range w.events {
			w.doc.Call("removeEventListener", name, w.stopInput, js.ValueOf(map[string]any{"capture": true}))
		}
		w.stopInput.Release()
		w.stopInput = js.Func{}
	}
	if w.tick.Truthy() {
		w.tick.Release()
		w.tick = js.Func{}
	}
}

func (w *readingWatch) step() {
	defer func() { _ = recover() }()
	if w.done {
		w.finish()
		return
	}
	row := findSearchRow(w.id)
	if !row.Truthy() {
		w.absent++
		if w.absent >= issue3989MissingFrames {
			w.done = true
			w.finish()
			if w.missing != nil {
				w.missing()
			}
			return
		}
	} else if list := row.Call("closest", "["+listAnchorAttr+"]"); list.Truthy() {
		delta := row.Call("getBoundingClientRect").Get("top").Float() - list.Call("getBoundingClientRect").Get("top").Float() - w.offset
		if delta > 1 || delta < -1 {
			markSearchAway(row)
			list.Set("scrollTop", list.Get("scrollTop").Float()+delta)
			list.Set("__chatScrollTop", list.Get("scrollTop"))
		}
		w.placed = true
	}
	if w.placed {
		w.settled++
	}
	if w.settled >= issue3989SettleFrames/4 {
		w.done = true
		w.finish()
		return
	}
	if !w.schedule() {
		w.done = true
		w.finish()
	}
}

// focusChatTimeline gives keyboard focus to the timeline once the bar that held
// it has gone.
func focusChatTimeline() {
	dateJumpFrame(func() {
		list := js.Global().Get("document").Call("querySelector", "["+listAnchorAttr+"]")
		if !list.Truthy() {
			return
		}
		if !list.Call("hasAttribute", "tabindex").Truthy() {
			list.Call("setAttribute", "tabindex", "-1")
		}
		list.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	})
}
