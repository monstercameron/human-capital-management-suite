//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

func TestChannelTodoSelectDOMReconcilesAfterModeAndMemberChange(t *testing.T) {
	global := js.Global()
	previous := global.Get("document")
	defer global.Set("document", previous)
	selectEl := global.Get("Object").New()
	selectEl.Set("value", "ME_AND_SELECTED")
	want := "EVERYONE"
	getAttribute := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].String() == selectValueAttr {
			return want
		}
		return js.Null()
	})
	defer getAttribute.Release()
	selectEl.Set("getAttribute", getAttribute)
	selects := global.Get("Array").New()
	selects.Call("push", selectEl)
	query := js.FuncOf(func(_ js.Value, args []js.Value) any { return selects })
	defer query.Release()
	doc := global.Get("Object").New()
	doc.Set("querySelectorAll", query)
	global.Set("document", doc)
	seedAllSelects()
	if got := selectEl.Get("value").String(); got != "EVERYONE" {
		t.Fatalf("existing policy select = %q", got)
	}
	selectEl.Set("value", "sam")
	want = "__none__"
	seedAllSelects()
	if got := selectEl.Get("value").String(); got != "" {
		t.Fatalf("member picker preselected %q", got)
	}
}

func TestChatListReplacementKeepsReaderPosition(t *testing.T) {
	oldList := lastChatList
	t.Cleanup(func() { lastChatList = oldList })
	lastChatList = js.Undefined()
	makeList := func(anchor string) (js.Value, js.Func, js.Func) {
		list := js.Global().Get("Object").New()
		classes := js.Global().Get("Object").New()
		toggle := js.FuncOf(func(js.Value, []js.Value) any { return nil })
		classes.Set("toggle", toggle)
		parent := js.Global().Get("Object").New()
		parent.Set("classList", classes)
		list.Set("parentElement", parent)
		list.Set("scrollHeight", 2000)
		list.Set("clientHeight", 500)
		list.Set("scrollTop", 0)
		list.Set("isConnected", true)
		getAnchor := js.FuncOf(func(js.Value, []js.Value) any { return anchor })
		list.Set("getAttribute", getAnchor)
		return list, getAnchor, toggle
	}
	first, firstAnchor, firstToggle := makeList("room:post-1")
	defer firstAnchor.Release()
	defer firstToggle.Release()
	keepListPlace(first)
	first.Set("scrollTop", 500)
	first.Set("__chatScrollTop", 500)
	first.Set("__chatNearBottom", false)
	first.Set("scrollTop", 0) // detached elements can lose their layout position

	replacement, nextAnchor, nextToggle := makeList("room:post-1")
	defer nextAnchor.Release()
	defer nextToggle.Release()
	keepListPlace(replacement)
	if got := replacement.Get("scrollTop").Int(); got != 500 || replacement.Get("__chatNearBottom").Truthy() {
		t.Fatalf("same-room replacement moved reader: scrollTop=%d nearBottom=%v", got, replacement.Get("__chatNearBottom"))
	}
	newTail, tailAnchor, tailToggle := makeList("room:post-2")
	defer tailAnchor.Release()
	defer tailToggle.Release()
	keepListPlace(newTail)
	if got := newTail.Get("scrollTop").Int(); got != 500 || newTail.Get("__chatNearBottom").Truthy() {
		t.Fatalf("same-room new tail moved reader: scrollTop=%d nearBottom=%v", got, newTail.Get("__chatNearBottom"))
	}
	newTail.Set("__chatNearBottom", true)
	bottomReplacement, bottomAnchor, bottomToggle := makeList("room:post-2")
	defer bottomAnchor.Release()
	defer bottomToggle.Release()
	keepListPlace(bottomReplacement)
	if got := bottomReplacement.Get("scrollTop").Int(); got != 2000 {
		t.Fatalf("same-room bottom replacement scrollTop=%d, want newest", got)
	}

	switched, switchedAnchor, switchedToggle := makeList("other:post-1")
	defer switchedAnchor.Release()
	defer switchedToggle.Release()
	keepListPlace(switched)
	if got := switched.Get("scrollTop").Int(); got != 2000 {
		t.Fatalf("new room scrollTop=%d, want newest", got)
	}
}

func TestPreserveTimelinePositionDisarmsBottomFollowing(t *testing.T) {
	global := js.Global()
	oldDocument := global.Get("document")
	defer global.Set("document", oldDocument)
	list := global.Get("Object").New()
	list.Set("scrollTop", 415)
	classes := global.Get("Object").New()
	var away bool
	toggle := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 2 && args[0].String() == "away" {
			away = args[1].Bool()
		}
		return nil
	})
	defer toggle.Release()
	classes.Set("toggle", toggle)
	parent := global.Get("Object").New()
	parent.Set("classList", classes)
	list.Set("parentElement", parent)
	query := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].String() == "["+listAnchorAttr+"]" {
			return list
		}
		return js.Null()
	})
	defer query.Release()
	doc := global.Get("Object").New()
	doc.Set("querySelector", query)
	global.Set("document", doc)

	PreserveTimelinePosition()
	if got := list.Get("__chatScrollTop").Int(); got != 415 {
		t.Fatalf("saved scrollTop = %d, want 415", got)
	}
	if !list.Get("__chatScrollAwayIntent").Bool() || list.Get("__chatNearBottom").Bool() || !away {
		t.Fatal("timeline still follows the newest page")
	}
}

func TestChatMutationBatchReadsScrollGeometryOnce(t *testing.T) {
	list := js.Global().Get("Object").New()
	list.Set("__chatNearBottom", true)
	list.Set("scrollTop", 0)
	reads := 0
	getHeight := js.FuncOf(func(js.Value, []js.Value) any { reads++; return 2000 })
	defer getHeight.Release()
	js.Global().Get("Object").Call("defineProperty", list, "scrollHeight", map[string]any{"get": getHeight})
	toggle := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	defer toggle.Release()
	classes := js.Global().Get("Object").New()
	classes.Set("toggle", toggle)
	parent := js.Global().Get("Object").New()
	parent.Set("classList", classes)
	list.Set("parentElement", parent)
	target := js.Global().Get("Object").New()
	target.Set("nodeType", 1)
	closest := js.FuncOf(func(js.Value, []js.Value) any { return list })
	defer closest.Release()
	target.Set("closest", closest)
	var grown []js.Value
	for i := 0; i < 500; i++ {
		grown = appendGrowingList(grown, target)
	}
	for _, item := range grown {
		scrollToEnd(item)
	}
	if len(grown) != 1 || reads != 1 {
		t.Fatalf("500 row mutations settled %d lists with %d layout reads, want one each", len(grown), reads)
	}
}
