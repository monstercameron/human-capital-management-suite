//go:build js && wasm

package chatui

import (
	"sync"
	"syscall/js"
	"time"
)

// Text fields in this package never carry a `Value` prop. GoWebComponents
// writes `value` as a property and decides whether to write by comparing the
// new prop with the PREVIOUS RENDER'S prop, never with what the box holds, so
// a render that lands after the next keystroke writes its own older string
// back and eats the character typed in between (measured in the chat
// composer: five seeded messages arrived as one concatenated smear).
//
// The application's idea of the value therefore travels as an attribute
// (data-chat-value), which the reconciler diffs and sets without touching
// `.value`. One document-level observer carries the attribute into the
// property when the application changed it — and refuses to while the box has
// focus and the user typed since the application last wrote, so the box, not
// a late render, decides what it says. A form that clears itself on send still
// clears, because sending resets the typed flag through the attribute change.
const fieldValueAttr = "data-chat-value"
const selectValueAttr = "data-chat-select-value"
const selectVersionAttr = "data-chat-select-version"

// listAnchorAttr is the timeline's "room:newest message" marker (listAnchor in
// render.go). The observer below keeps the reader's place from it: opening a
// room lands on the newest message, and a message arriving while the reader is
// at the bottom keeps them there, while a reader who scrolled up to read is
// never yanked back down.
const listAnchorAttr = "data-chat-anchor"

// nearBottomPx is how close to the end still counts as "reading the newest".
const nearBottomPx = 120

var fieldSyncOnce sync.Once
var lastChatList js.Value

var newestJump struct {
	room, principal string
	until           time.Time
	serial          uint64
	active          bool
}

func clearChatScrollMemory() { lastChatList = js.Undefined(); cancelNewestJump() }

func cancelNewestJump() { newestJump.active = false; newestJump.serial++ }

func jumpPendingForRoom(room string) bool {
	if !newestJump.active || room == "" || room != newestJump.room || time.Now().After(newestJump.until) {
		return false
	}
	doc := js.Global().Get("document")
	if !doc.Truthy() || doc.Get("querySelector").Type() != js.TypeFunction {
		return false
	}
	workspace := doc.Call("querySelector", ".chat-workspace")
	return workspace.Truthy() && workspace.Get("dataset").Get("principal").String() == newestJump.principal
}

func installFieldSync() {
	fieldSyncOnce.Do(func() {
		doc := js.Global().Get("document")
		if !doc.Truthy() {
			return
		}
		typed := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			if t := args[0].Get("target"); t.Truthy() {
				t.Set("__chatTyped", true)
			}
			return nil
		})
		doc.Call("addEventListener", "input", typed, true)
		selectEdited := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			el := args[0].Get("target")
			if !el.Truthy() || el.Get("getAttribute").IsUndefined() || el.Call("getAttribute", "data-chat-select-editable").String() != "true" {
				return nil
			}
			el.Set("__chatSelectEditedVersion", el.Call("getAttribute", selectVersionAttr))
			return nil
		})
		doc.Call("addEventListener", "input", selectEdited, true)
		doc.Call("addEventListener", "change", selectEdited, true)
		scrolled := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			if t := args[0].Get("target"); t.Truthy() && t.Get("getAttribute").Truthy() && t.Call("hasAttribute", listAnchorAttr).Truthy() {
				previous := t.Get("__chatScrollTop")
				if t.Get("__chatManualScrollArmed").Truthy() && previous.Type() == js.TypeNumber && t.Get("scrollTop").Float() < previous.Float()-1 {
					setChatScrollAway(t)
				}
				t.Set("__chatManualScrollArmed", false)
				near := nearBottom(t)
				t.Set("__chatNearBottom", near)
				t.Set("__chatScrollTop", t.Get("scrollTop"))
				markAway(t, !near)
			}
			return nil
		})
		doc.Call("addEventListener", "scroll", scrolled, true)
		cancelJumpInput := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				e := args[0]
				if list := listOf(e.Get("target")); list.Truthy() {
					switch e.Get("type").String() {
					case "wheel":
						if e.Get("deltaY").Float() < 0 {
							setChatScrollAway(list)
						}
					case "touchstart", "pointerdown":
						list.Set("__chatManualScrollArmed", true)
					case "keydown":
						key := e.Get("key").String()
						if key == "ArrowUp" || key == "PageUp" || key == "Home" {
							setChatScrollAway(list)
						}
					}
				}
				if newestJump.active {
					cancelNewestJump()
				}
			}
			return nil
		})
		for _, event := range []string{"wheel", "touchstart", "pointerdown", "keydown"} {
			doc.Call("addEventListener", event, cancelJumpInput, true)
		}
		// An image decoding after its bytes arrive grows the row without any
		// DOM change, so a reader parked at the bottom would drift up by the
		// image's height. Load events do not bubble; capture catches them.
		loaded := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			t := args[0].Get("target")
			if !t.Truthy() || t.Get("tagName").String() != "IMG" {
				return nil
			}
			if list := listOf(t); list.Truthy() && list.Get("__chatNearBottom").Truthy() {
				scrollToEnd(list)
			}
			return nil
		})
		doc.Call("addEventListener", "load", loaded, true)
		obs := js.Global().Get("MutationObserver")
		if !obs.Truthy() {
			return
		}
		var sweep js.Func
		pending := false
		sweep = js.FuncOf(func(js.Value, []js.Value) any {
			pending = false
			seedAllFields()
			seedAllSelects()
			keepAllListPlaces()
			syncAllPaneProperties()
			return nil
		})
		cb := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			records := args[0]
			needSweep := false
			grownLists := []js.Value{}
			for i := 0; i < records.Length(); i++ {
				r := records.Index(i)
				if r.Get("type").String() == "attributes" {
					switch r.Get("attributeName").String() {
					case listAnchorAttr:
						keepListPlace(r.Get("target"))
					case paneRailAttr, paneDetailsAttr:
						applyPaneAttribute(r.Get("target"), r.Get("attributeName").String())
					case selectValueAttr, selectVersionAttr:
						applySelectValue(r.Get("target"))
					default:
						applyFieldValue(r.Get("target"))
					}
					continue
				}
				needSweep = true
				grownLists = appendGrowingList(grownLists, r.Get("target"))
			}
			// A timeline update can produce hundreds of child-list records.
			// Reading scrollHeight after each one forces repeated layout; settle
			// each affected list once after the complete mutation batch.
			for _, list := range grownLists {
				if list.Get("__chatNearBottom").Truthy() {
					scrollToEnd(list)
				}
			}
			if needSweep && !pending {
				pending = true
				js.Global().Call("setTimeout", sweep, 0)
			}
			return nil
		})
		observer := obs.New(cb)
		observer.Call("observe", doc.Get("documentElement"), map[string]any{
			"subtree": true, "childList": true, "attributes": true, "attributeOldValue": true,
			"attributeFilter": []any{fieldValueAttr, selectValueAttr, selectVersionAttr, listAnchorAttr, paneRailAttr, paneDetailsAttr},
		})
		seedAllFields()
		seedAllSelects()
		keepAllListPlaces()
	})
}

// appendGrowingList deduplicates the lists touched by one mutation callback.
// Scroll geometry is read only after that callback has seen every row change.
func appendGrowingList(lists []js.Value, target js.Value) []js.Value {
	list := listOf(target)
	if !list.Truthy() || !list.Get("__chatNearBottom").Truthy() {
		return lists
	}
	for _, grown := range lists {
		if list.Equal(grown) {
			return lists
		}
	}
	return append(lists, list)
}

// keepAllListPlaces visits every timeline on the page. A list the reconciler
// created with its anchor already set produces no attribute record, only a
// child-list one, so the sweep after any structural change checks them all.
func keepAllListPlaces() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	lists := doc.Call("querySelectorAll", "["+listAnchorAttr+"]")
	for i := 0; i < lists.Length(); i++ {
		keepListPlace(lists.Index(i))
	}
}

// keepListPlace reacts to the timeline's anchor changing, compared with the
// anchor this element was last settled at (kept on the element, because the
// reconciler reuses the keyed list across rooms). A different room, or the
// first content of one, lands on the newest message; a new tail in the same
// room follows only when the reader was at the bottom already.
func keepListPlace(list js.Value) {
	if !list.Truthy() {
		return
	}
	now := list.Call("getAttribute", listAnchorAttr)
	if now.Type() != js.TypeString || now.String() == "" {
		return
	}
	settled := list.Get("__chatAnchor")
	replaced := false
	if lastChatList.Truthy() && !list.Equal(lastChatList) {
		old := lastChatList
		oldAnchor := old.Get("__chatAnchor")
		if oldAnchor.Type() == js.TypeString && chatAnchorRoom(oldAnchor.String()) == chatAnchorRoom(now.String()) {
			// A route refresh may replace the keyed list. Carry the reader's
			// position to the new element before treating it as a new room.
			settled = oldAnchor
			replaced = true
			list.Set("__chatNearBottom", old.Get("__chatNearBottom"))
			if !old.Get("__chatNearBottom").Truthy() {
				position := old.Get("__chatScrollTop")
				if position.IsUndefined() {
					position = old.Get("scrollTop")
				}
				list.Set("scrollTop", position)
				list.Set("__chatScrollTop", position)
				markAway(list, true)
			}
		}
	}
	lastChatList = list
	if settled.Type() == js.TypeString && settled.String() == now.String() {
		list.Set("__chatAnchor", now.String())
		if replaced && list.Get("__chatNearBottom").Truthy() {
			scrollToEnd(list)
		}
		return
	}
	list.Set("__chatAnchor", now.String())
	room := chatAnchorRoom(now.String())
	previous := ""
	if settled.Type() == js.TypeString {
		previous = chatAnchorRoom(settled.String())
	}
	switched := previous != room
	if switched || list.Get("__chatNearBottom").IsUndefined() || list.Get("__chatNearBottom").Truthy() {
		scrollToEnd(list)
		if switched {
			// Images and late rows land after the first paint; settle again
			// once the frame after next has laid them out.
			raf := js.Global().Get("requestAnimationFrame")
			if raf.Truthy() {
				var again js.Func
				again = js.FuncOf(func(js.Value, []js.Value) any {
					// A reader may scroll during this frame, or open another room.
					// Neither action should be undone by the earlier room's settle.
					if list.Get("isConnected").Truthy() && list.Get("__chatNearBottom").Truthy() && list.Call("getAttribute", listAnchorAttr).String() == now.String() {
						scrollToEnd(list)
					}
					again.Release()
					return nil
				})
				raf.Invoke(again)
			}
		}
	}
}

func chatAnchorRoom(anchor string) string {
	if i := indexByte(anchor, ':'); i >= 0 {
		return anchor[:i]
	}
	return anchor
}

// listOf finds the timeline that contains a mutated node, if any.
func listOf(node js.Value) js.Value {
	if !node.Truthy() {
		return js.Undefined()
	}
	el := node
	if el.Get("nodeType").Int() != 1 {
		el = el.Get("parentElement")
	}
	if !el.Truthy() || !el.Get("closest").Truthy() {
		return js.Undefined()
	}
	return el.Call("closest", "["+listAnchorAttr+"]")
}

func nearBottom(el js.Value) bool {
	gap := el.Get("scrollHeight").Float() - el.Get("scrollTop").Float() - el.Get("clientHeight").Float()
	if gap <= 2 {
		el.Set("__chatScrollAwayIntent", false)
		return true
	}
	return !el.Get("__chatScrollAwayIntent").Truthy() && gap < nearBottomPx
}

func setChatScrollAway(el js.Value) {
	el.Set("__chatScrollAwayIntent", true)
	el.Set("__chatNearBottom", false)
	markAway(el, true)
}

func scrollToEnd(el js.Value) {
	el.Set("__chatScrollAwayIntent", false)
	el.Set("scrollTop", el.Get("scrollHeight"))
	el.Set("__chatScrollTop", el.Get("scrollTop"))
	el.Set("__chatNearBottom", true)
	markAway(el, false)
}

func BeginScrollToNewest() {
	doc := js.Global().Get("document")
	if !doc.Truthy() || doc.Get("querySelector").Type() != js.TypeFunction {
		return
	}
	list := doc.Call("querySelector", "["+listAnchorAttr+"]")
	workspace := doc.Call("querySelector", ".chat-workspace")
	if !list.Truthy() || !workspace.Truthy() {
		return
	}
	anchor := list.Call("getAttribute", listAnchorAttr)
	if anchor.Type() != js.TypeString {
		return
	}
	newestJump.room = chatAnchorRoom(anchor.String())
	newestJump.principal = workspace.Get("dataset").Get("principal").String()
	newestJump.until = time.Now().Add(35 * time.Second)
	newestJump.serial++
	newestJump.active = true
	scrollToEnd(list)
}

// markAway flags the timeline's section while the reader is scrolled up, which
// is what shows the "Jump to newest" control (styles.go, .chat-main.away).
func markAway(list js.Value, away bool) {
	parent := list.Get("parentElement")
	if !parent.Truthy() {
		return
	}
	parent.Get("classList").Call("toggle", "away", away)
}

// ScrollToNewest takes the reader to the newest message of the open room.
func ScrollToNewest() {
	doc := js.Global().Get("document")
	if !doc.Truthy() || !newestJump.active || doc.Get("querySelector").Type() != js.TypeFunction {
		return
	}
	serial := newestJump.serial
	raf := js.Global().Get("requestAnimationFrame")
	if raf.Type() != js.TypeFunction {
		return
	}
	frames, stable := 0, 0
	var settle js.Func
	settle = js.FuncOf(func(js.Value, []js.Value) any {
		list := doc.Call("querySelector", "["+listAnchorAttr+"]")
		if !newestJump.active || newestJump.serial != serial || !list.Truthy() || !jumpPendingForRoom(chatAnchorRoom(list.Call("getAttribute", listAnchorAttr).String())) || frames >= 180 {
			settle.Release()
			return nil
		}
		frames++
		if list.Get("dataset").Get("hasNewer").String() != "true" {
			scrollToEnd(list)
			gap := list.Get("scrollHeight").Float() - list.Get("scrollTop").Float() - list.Get("clientHeight").Float()
			if gap <= 1 {
				stable++
			} else {
				stable = 0
			}
			if stable >= 3 {
				cancelNewestJump()
				settle.Release()
				return nil
			}
		}
		raf.Invoke(settle)
		return nil
	})
	raf.Invoke(settle)
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func seedAllFields() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	found := doc.Call("querySelectorAll", "["+fieldValueAttr+"]")
	for i := 0; i < found.Length(); i++ {
		applyFieldValue(found.Index(i))
	}
}

// Select option nodes can be reused by the reconciler after a room or policy
// change. Keep the live browser property aligned with the rendered model.
func seedAllSelects() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	found := doc.Call("querySelectorAll", "["+selectValueAttr+"]")
	for i := 0; i < found.Length(); i++ {
		applySelectValue(found.Index(i))
	}
}

func applySelectValue(el js.Value) {
	if !el.Truthy() || el.Get("getAttribute").IsUndefined() {
		return
	}
	want := el.Call("getAttribute", selectValueAttr)
	if want.Type() != js.TypeString {
		return
	}
	value := want.String()
	if el.Call("getAttribute", "data-chat-select-editable").String() == "true" {
		version := el.Call("getAttribute", selectVersionAttr)
		if version.Type() == js.TypeString && el.Get("__chatSelectEditedVersion").Type() == js.TypeString && el.Get("__chatSelectEditedVersion").String() == version.String() {
			return
		}
		el.Set("__chatSelectEditedVersion", js.Undefined())
	}
	if value == "__none__" {
		value = ""
	}
	if el.Get("value").String() != value {
		el.Set("value", value)
	}
}

func applyFieldValue(el js.Value) {
	if !el.Truthy() || el.Get("getAttribute").IsUndefined() {
		return
	}
	want := el.Call("getAttribute", fieldValueAttr)
	if want.Type() != js.TypeString {
		return
	}
	v := want.String()
	if el.Get("value").String() == v {
		el.Set("__chatTyped", false)
		return
	}
	doc := js.Global().Get("document")
	focused := doc.Truthy() && doc.Get("activeElement").Equal(el)
	// While the box has focus, the application may only CLEAR it (a send) or
	// fill an EMPTY box (a draft arriving). A non-empty replacement of
	// non-empty text arriving mid-typing is by definition a stale snapshot of
	// what the user already typed, and writing it back eats the tail
	// (measured: "last one lands." became "last one la").
	if focused && (el.Get("__chatTyped").Truthy() || (v != "" && el.Get("value").String() != "")) {
		return
	}
	// After the application cleared the box (a send), a late render still
	// carrying the old draft must not put it back; only the user's typing or
	// a render that agrees the box is empty ends the cleared state.
	if el.Get("__chatCleared").Truthy() {
		if v == "" {
			el.Set("__chatCleared", false)
		}
		return
	}
	el.Set("value", v)
	if focused {
		// Keep the caret at the end of the new text instead of wherever the
		// browser left it; the only application writes here are a clear after
		// send and a draft swap on conversation switch.
		n := len([]rune(v))
		func() {
			defer func() { _ = recover() }()
			el.Call("setSelectionRange", n, n)
		}()
	}
	el.Set("__chatTyped", false)
}
