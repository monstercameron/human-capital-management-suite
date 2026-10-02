//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
	"time"
)

func TestAgentUXChat4_M4_ResizePinsNewest(t *testing.T) {
	global := js.Global()
	lists := global.Get("Array").New()
	var away bool
	toggle := js.FuncOf(func(_ js.Value, args []js.Value) any { away = args[1].Bool(); return nil })
	defer toggle.Release()
	classes := global.Get("Object").New()
	classes.Set("toggle", toggle)
	parent := global.Get("Object").New()
	parent.Set("classList", classes)
	list := global.Get("Object").New()
	list.Set("parentElement", parent)
	list.Set("scrollHeight", 1500)
	list.Set("clientHeight", 500)
	list.Set("scrollTop", 1000)
	lists.Call("push", list)
	query := js.FuncOf(func(js.Value, []js.Value) any { return lists })
	defer query.Release()
	doc := global.Get("Object").New()
	doc.Set("querySelectorAll", query)
	for _, height := range []int{1800, 2200, 2000} { // answer, narrower thread, closing thread
		list.Set("scrollHeight", height)
		pinResizedChatLists(doc)
		if list.Get("scrollTop").Int() != height || away || !list.Get("__chatNearBottom").Truthy() {
			t.Fatal("late content or pane resize left the answer under the composer")
		}
	}
	list.Set("scrollTop", 700)
	setChatScrollAway(list)
	list.Set("scrollHeight", 2600)
	pinResizedChatLists(doc)
	if list.Get("scrollTop").Int() != 700 || !away {
		t.Fatal("late answer yanked the reader away from older messages or hid Jump to newest")
	}
	anchor := "general:question"
	getAnchor := js.FuncOf(func(js.Value, []js.Value) any { return anchor })
	defer getAnchor.Release()
	list.Set("getAttribute", getAnchor)
	pinResizedChatLists(doc)
	if list.Get("scrollTop").Int() != 2600 || away {
		t.Fatal("new thread did not reveal its newest answer")
	}
	list.Set("scrollTop", 700)
	setChatScrollAway(list)
	pinResizedChatLists(doc)
	if list.Get("scrollTop").Int() != 700 {
		t.Fatal("thread answer update ignored manual scrolling")
	}
	anchor = "general:next-question"
	pinResizedChatLists(doc)
	if list.Get("scrollTop").Int() != 2600 {
		t.Fatal("opening a different thread inherited the old thread's scroll-away state")
	}
	oldDoc := global.Get("document")
	defer global.Set("document", oldDoc)
	find := js.FuncOf(func(js.Value, []js.Value) any { return list })
	defer find.Release()
	doc.Set("querySelector", find)
	global.Set("document", doc)
	list.Set("scrollTop", 700)
	setChatScrollAway(list)
	pinAgentChatAfterSend("thread-composer")
	if list.Get("scrollTop").Int() != 2600 || away {
		t.Fatal("sending a reply while reading older replies did not reveal the new message")
	}
}

func TestAgentUXChat4_M11_FocusComposer(t *testing.T) {
	global := js.Global()
	oldDoc, oldRAF := global.Get("document"), global.Get("requestAnimationFrame")
	defer func() { global.Set("document", oldDoc); global.Set("requestAnimationFrame", oldRAF) }()
	focused := false
	input := global.Get("Object").New()
	focus := js.FuncOf(func(_ js.Value, args []js.Value) any { focused = args[0].Get("preventScroll").Bool(); return nil })
	defer focus.Release()
	input.Set("focus", focus)
	find := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() != "chat-composer" {
			t.Fatal("follow-up chose another field")
		}
		return input
	})
	defer find.Release()
	doc := global.Get("Object").New()
	doc.Set("getElementById", find)
	global.Set("document", doc)
	var callback js.Value
	raf := js.FuncOf(func(_ js.Value, args []js.Value) any { callback = args[0]; return nil })
	defer raf.Release()
	global.Set("requestAnimationFrame", raf)
	focusAgentChatComposer()
	if focused || callback.Type() != js.TypeFunction {
		t.Fatal("focus did not wait for the rendered conversation")
	}
	callback.Invoke()
	if !focused {
		t.Fatal("follow-up failed to focus the composer")
	}
}

func TestAgentUXChat4_M16_LongPress(t *testing.T) {
	global := js.Global()
	oldDoc := global.Get("document")
	defer func() {
		global.Set("document", oldDoc)
	}()
	listeners := map[string]js.Value{}
	add := js.FuncOf(func(_ js.Value, args []js.Value) any { listeners[args[0].String()] = args[1]; return nil })
	defer add.Release()
	remove := js.FuncOf(func(_ js.Value, args []js.Value) any { delete(listeners, args[0].String()); return nil })
	defer remove.Release()
	query := js.FuncOf(func(js.Value, []js.Value) any { return js.Null() })
	defer query.Release()
	doc := global.Get("Object").New()
	doc.Set("addEventListener", add)
	doc.Set("removeEventListener", remove)
	doc.Set("querySelector", query)
	global.Set("document", doc)
	workspace := global.Get("Object").New()
	workspace.Set("dataset", js.ValueOf(map[string]any{"selectedId": "general"}))
	row := global.Get("Object").New()
	row.Set("dataset", js.ValueOf(map[string]any{"messageId": "question"}))
	row.Set("isConnected", true)
	rowClosest := js.FuncOf(func(js.Value, []js.Value) any { return workspace })
	defer rowClosest.Release()
	row.Set("closest", rowClosest)
	interactive := false
	target := global.Get("Object").New()
	closest := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() == "button,a,input,textarea,summary" {
			if interactive {
				return target
			}
			return js.Null()
		}
		return row
	})
	defer closest.Release()
	target.Set("closest", closest)
	event := global.Get("Object").New()
	event.Set("target", target)
	event.Set("pointerType", "touch")
	event.Set("clientX", 10)
	event.Set("clientY", 10)
	opened := ""
	cleanup := bindChatMessageLongPress(Model{SelectedID: "general", Callbacks: Callbacks{OpenMenu: func(id string) { opened = id }}})
	listeners["pointerdown"].Invoke(event)
	if opened != "" {
		t.Fatal("tap opened actions without a hold")
	}
	event.Set("clientX", 30)
	listeners["pointermove"].Invoke(event)
	time.Sleep(600 * time.Millisecond)
	if opened != "" {
		t.Fatal("scroll gesture opened actions")
	}
	event.Set("clientX", 10)
	listeners["pointerdown"].Invoke(event)
	time.Sleep(600 * time.Millisecond)
	if opened != "question" {
		t.Fatal("long press did not open the pressed message's actions")
	}
	opened = ""
	interactive = true
	listeners["pointerdown"].Invoke(event)
	if opened != "" {
		t.Fatal("long press hijacked an interactive control")
	}
	cleanup()
	if len(listeners) != 0 {
		t.Fatal("long-press listeners survived unmount")
	}
}
