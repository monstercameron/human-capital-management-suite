//go:build js && wasm

package chatui

import (
	"syscall/js"
	"time"
)

// FocusSearchMessage puts a search hit in view and gives keyboard focus to its
// article after the next render has attached the targeted timeline page.
func FocusSearchMessage(id string) {
	FocusSearchMessageOrElse(id, nil)
}

// FocusSearchMessageOrElse is FocusSearchMessage that also says when the hit
// never reached the page (deleted, no longer visible to the reader, or its page
// failed to load): missing runs once, after the wait, so the reader is told
// instead of being left on unrelated messages.
//
// The row is looked up on every animation frame until it exists, because a hit
// opened from another channel is requested before the destination's timeline is
// drawn. Once focused it is kept in view for a short settle window: the room
// switch lands the list on its newest message after the first focus, and images
// change row heights afterwards (#3989). The watch ends at once when the reader
// scrolls, touches or presses a key.
func FocusSearchMessageOrElse(id string, missing func()) {
	if id == "" {
		return
	}
	global := js.Global()
	request := global.Get("requestAnimationFrame")
	if !request.Truthy() {
		focusSearchMessageNow(id)
		return
	}
	watch := &searchWatch{id: id, missing: missing}
	watch.listen(global.Get("document"))
	watch.tick = js.FuncOf(func(js.Value, []js.Value) any {
		watch.step()
		return nil
	})
	if !watch.schedule() {
		watch.finish()
		focusSearchMessageNow(id)
	}
}

type searchWatch struct {
	id        string
	missing   func()
	tick      js.Func
	frames    int
	settled   int
	absent    int
	focused   bool
	done      bool
	doc       js.Value
	stopInput js.Func
	events    []string
}

func (w *searchWatch) schedule() (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	js.Global().Call("requestAnimationFrame", w.tick)
	return true
}

// listen ends the watch when the reader takes over the scroll position.
func (w *searchWatch) listen(doc js.Value) {
	if !doc.Truthy() || doc.Get("addEventListener").Type() != js.TypeFunction {
		return
	}
	w.doc = doc
	w.events = []string{"wheel", "touchstart", "keydown", "pointerdown"}
	w.stopInput = js.FuncOf(func(js.Value, []js.Value) any {
		w.done = true
		return nil
	})
	for _, name := range w.events {
		doc.Call("addEventListener", name, w.stopInput, js.ValueOf(map[string]any{"passive": true, "capture": true}))
	}
}

func (w *searchWatch) finish() {
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

func (w *searchWatch) step() {
	defer func() { _ = recover() }()
	if w.done {
		w.finish()
		return
	}
	w.frames++
	row := findSearchRow(w.id)
	switch {
	case !row.Truthy():
		w.absent++
		if w.absent >= issue3989MissingFrames {
			w.done = true
			w.finish()
			if w.missing != nil {
				w.missing()
			}
			return
		}
	case !w.focused:
		w.focused = true
		focusSearchRow(row)
	case searchRowOutOfView(row):
		row.Call("scrollIntoView", js.ValueOf(map[string]any{"block": "center"}))
		markSearchAway(row)
	}
	if w.focused {
		w.settled++
	}
	if w.settled >= issue3989SettleFrames {
		w.done = true
		w.finish()
		return
	}
	if !w.schedule() {
		w.done = true
		w.finish()
	}
}

func findSearchRow(id string) js.Value {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return js.Undefined()
	}
	rows := doc.Call("querySelectorAll", "[data-message-id]")
	for i := 0; i < rows.Length(); i++ {
		row := rows.Index(i)
		if row.Get("dataset").Get("messageId").String() == id {
			return row
		}
	}
	return js.Undefined()
}

// searchRowOutOfView reads the row's and its scroller's rectangles; a page that
// cannot measure them is treated as in view so nothing is scrolled blindly.
func searchRowOutOfView(row js.Value) bool {
	if row.Get("closest").Type() != js.TypeFunction || row.Get("getBoundingClientRect").Type() != js.TypeFunction {
		return false
	}
	scroller := row.Call("closest", "["+listAnchorAttr+"],.thread-scroll")
	if !scroller.Truthy() || scroller.Get("getBoundingClientRect").Type() != js.TypeFunction {
		return false
	}
	r, l := row.Call("getBoundingClientRect"), scroller.Call("getBoundingClientRect")
	return issue3989NeedsRecenter(r.Get("top").Float(), r.Get("bottom").Float(), l.Get("top").Float(), l.Get("bottom").Float())
}

func focusSearchMessageNow(id string) {
	defer func() { _ = recover() }()
	if row := findSearchRow(id); row.Truthy() {
		focusSearchRow(row)
	}
}

// markSearchAway tells the timeline the reader is away from the newest message,
// so the follow-the-bottom rules leave the hit where it is.
func markSearchAway(row js.Value) {
	if row.Get("closest").Type() == js.TypeFunction {
		list := row.Call("closest", "["+listAnchorAttr+"]")
		if list.Truthy() {
			list.Set("__chatScrollAwayIntent", true)
			list.Set("__chatNearBottom", false)
			list.Set("__chatScrollTop", list.Get("scrollTop"))
			markAway(list, true)
		}
	}
}

func focusSearchRow(row js.Value) {
	markSearchAway(row)
	row.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	row.Call("scrollIntoView", js.ValueOf(map[string]any{"block": "center"}))
	row.Get("classList").Call("add", "search-target")
	var remove js.Func
	remove = js.FuncOf(func(js.Value, []js.Value) any {
		if row.Get("isConnected").Truthy() {
			row.Get("classList").Call("remove", "search-target")
		}
		remove.Release()
		return nil
	})
	js.Global().Call("setTimeout", remove, int(time.Second*3/time.Millisecond))
}
