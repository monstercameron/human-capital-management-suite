//go:build js && wasm

package main

import (
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATSAVE-002. The redesigned Saved panel's interactions. Every one is a click
// or a key press handled here from the document, so none depends on focus
// events; the one timer only removes the Undo line.

const chatsaveUndoWindow = 6 * time.Second

func chatsaveNarrow() bool { return js.Global().Get("innerWidth").Float() <= 760 }

func (b *chatsaveBrowser) panel() js.Value {
	return js.Global().Get("document").Call("getElementById", "chatsave-list")
}

// selectTab shows one of To do, Done and All.
func (b *chatsaveBrowser) selectTab(tab string) {
	b.tab = tab
	b.errorCode, b.commandFailed = "", false
	b.reminderMenu, b.editingNote, b.pickingDate = "", "", false
	b.render()
}

// focusAfterRemoval picks where focus goes when an item leaves the list: the
// next item, else the one before it, else the selected segment.
func (b *chatsaveBrowser) focusAfterRemoval(item js.Value) {
	b.focusID = "chatsave-tab-" + b.tab
	if !item.Truthy() {
		return
	}
	for _, sibling := range []string{"nextElementSibling", "previousElementSibling"} {
		if next := item.Get(sibling); next.Truthy() && next.Get("id").String() != "" {
			b.focusID = next.Get("id").String()
			return
		}
	}
}

// startUndo puts the Undo line up for six seconds. The change itself has
// already been made; the timer only takes the line away.
func (b *chatsaveBrowser) startUndo(kind, host string, item chat.SavedItem) {
	b.undo = &chatsaveUndo{kind: kind, host: host, item: item}
	b.undoToken++
	token := b.undoToken
	go func() {
		time.Sleep(chatsaveUndoWindow)
		ui.PostAsync(func() {
			if !b.disposed && b.undoToken == token && b.undo != nil {
				b.undo = nil
				b.render()
			}
		})
	}()
}

// runUndo reverses the last done or remove: the item is reopened, or saved
// again with the note, reminder and state it had.
func (b *chatsaveBrowser) runUndo() {
	undo := b.undo
	if undo == nil {
		return
	}
	b.undo = nil
	b.undoToken++
	b.commandFailed = false
	switch undo.kind {
	case "done":
		command := chatsaveCommand{Action: "reopen", ConversationID: undo.item.ConversationID, PostID: undo.item.PostID}
		b.applyLocal(undo.host, command)
		b.command(undo.host, command)
	case "remove":
		restored := undo.item
		commands := chatsaveRestore(restored)
		b.page = chatsaveApplyLocal(b.page, undo.host, commands[0], &restored)
		b.localEpoch++
		b.marks()
		for _, command := range commands {
			b.command(undo.host, command)
		}
	}
	b.focusID = "chatsave-item-" + undo.item.PostID
	b.render()
}

// dueCommand sets a reminder (or clears it when at is nil) and closes the menu.
func (b *chatsaveBrowser) dueCommand(host, conversationID, postID string, at *time.Time) {
	command := chatsaveCommand{Action: "due", ConversationID: conversationID, PostID: postID, SetDue: true, DueAt: at}
	b.reminderMenu, b.pickingDate = "", false
	b.focusID = "chatsave-remind-" + postID
	if b.commandFailed {
		b.errorCode = ""
	}
	b.commandFailed = false
	b.applyLocal(host, command)
	b.command(host, command)
}

func chatsavePostOf(key string) string {
	if at := strings.LastIndex(key, "|"); at >= 0 {
		return key[at+1:]
	}
	return key
}

// closeMenu closes the reminder menu and, for the keyboard, returns focus to the bell.
func (b *chatsaveBrowser) closeMenu(refocus bool) {
	post := chatsavePostOf(b.reminderMenu)
	b.reminderMenu, b.pickingDate = "", false
	if refocus && post != "" {
		b.focusID = "chatsave-remind-" + post
	}
	b.render()
}

// cancelNote leaves the note field without saving and returns focus to the pencil.
func (b *chatsaveBrowser) cancelNote() {
	post := chatsavePostOf(b.editingNote)
	b.editingNote = ""
	if post != "" {
		b.focusID = "chatsave-note-" + post
	}
	b.render()
}

// measure shows "Show more" only on text that is cut, and fades the cut text.
// The markup guesses from the length of the text; this reads the drawn height.
func (b *chatsaveBrowser) measure(mount js.Value) {
	texts := mount.Call("querySelectorAll", ".chatsave-text")
	for i := 0; i < texts.Length(); i++ {
		text := texts.Index(i)
		if text.Get("clientHeight").Float() == 0 {
			continue
		}
		expanded := text.Get("classList").Call("contains", "is-expanded").Bool()
		overflow := text.Get("scrollHeight").Float() > text.Get("clientHeight").Float()+1
		if overflow && !expanded {
			text.Call("setAttribute", "data-clamped", "true")
		} else {
			text.Call("removeAttribute", "data-clamped")
		}
		if more := text.Get("nextElementSibling"); more.Truthy() && more.Get("classList").Call("contains", "chatsave-more").Bool() {
			more.Set("hidden", !(overflow || expanded))
		}
	}
}

// placeMenu keeps the open reminder menu where the person can use it. The menu
// is the bell's child and opens under the bell; here it is measured against the
// panel and the window, flipped above the bell when there is no room below, and
// slid sideways until it is inside the panel.
func (b *chatsaveBrowser) placeMenu(mount js.Value) {
	menu := mount.Call("querySelector", "[data-saved-menu]")
	if !menu.Truthy() {
		return
	}
	menu.Get("classList").Call("remove", "is-above")
	menu.Get("style").Call("removeProperty", "--chatsave-menu-shift")
	rect := menu.Call("getBoundingClientRect")
	anchor := menu.Get("parentElement").Call("getBoundingClientRect")
	panel := mount.Call("getBoundingClientRect")
	top, bottom := panel.Get("top").Float(), panel.Get("bottom").Float()
	if header := mount.Call("querySelector", ".chatsave-top"); header.Truthy() {
		if edge := header.Call("getBoundingClientRect").Get("bottom").Float(); edge > top {
			top = edge
		}
	}
	if height := js.Global().Get("innerHeight").Float(); bottom > height {
		bottom = height
	}
	top, bottom = top+8, bottom-8
	height := rect.Get("height").Float()
	below := bottom - rect.Get("top").Float()
	above := anchor.Get("top").Float() - 6 - top
	if rect.Get("bottom").Float() > bottom && (above >= height || above > below) {
		menu.Get("classList").Call("add", "is-above")
	}
	left, right := panel.Get("left").Float()+8, panel.Get("right").Float()-8
	if width := js.Global().Get("innerWidth").Float(); right > width-8 {
		right = width - 8
	}
	shift := 0.0
	if rect.Get("right").Float() > right {
		shift = right - rect.Get("right").Float()
	}
	if rect.Get("left").Float()+shift < left {
		shift = left - rect.Get("left").Float()
	}
	if shift != 0 {
		menu.Get("style").Call("setProperty", "--chatsave-menu-shift", strconv.FormatFloat(shift, 'f', 1, 64)+"px")
	}
	if after := menu.Call("getBoundingClientRect"); after.Get("bottom").Float() > bottom || after.Get("top").Float() < top {
		// Neither side has room: bring the menu into view by scrolling the panel.
		menu.Call("scrollIntoView", map[string]any{"block": "nearest"})
	}
}

// panelKey handles the keys the Saved panel owns while focus is inside it. It
// reports whether it used the key.
func (b *chatsaveBrowser) panelKey(event, target js.Value) bool {
	if event.Get("isComposing").Truthy() {
		return false
	}
	b.pointer = false
	key := event.Get("key").String()
	modified := event.Get("ctrlKey").Bool() || event.Get("metaKey").Bool() || event.Get("altKey").Bool()
	editable := target.Call("closest", "input,textarea,select,[contenteditable='true']").Truthy()
	item := target.Call("closest", "[data-saved-item]")
	stop := func() {
		event.Call("preventDefault")
		event.Call("stopPropagation")
	}
	panel := b.panel()
	switch key {
	case "Escape":
		if b.reminderMenu != "" {
			stop()
			b.closeMenu(true)
			return true
		}
		if b.editingNote != "" {
			stop()
			b.cancelNote()
			return true
		}
		return false
	case "ArrowDown", "ArrowUp":
		if modified || event.Get("shiftKey").Bool() {
			return false
		}
		step := 1
		if key == "ArrowUp" {
			step = -1
		}
		if menu := target.Call("closest", "[data-saved-menu]"); menu.Truthy() {
			stop()
			chatsaveFocusStep(menu.Call("querySelectorAll", "button.chatsave-menu-item"), target, step)
			return true
		}
		if editable && target.Get("id").String() != "chatsave-search" {
			return false
		}
		items := panel.Call("querySelectorAll", "[data-saved-item]")
		if items.Length() == 0 {
			return false
		}
		stop()
		current := -1
		for i := 0; i < items.Length(); i++ {
			if item.Truthy() && items.Index(i).Equal(item) {
				current = i
			}
		}
		next := current + step
		if current < 0 {
			next = 0
			if step < 0 {
				next = items.Length() - 1
			}
		}
		if next < 0 {
			next = 0
		}
		if next >= items.Length() {
			next = items.Length() - 1
		}
		focus := items.Index(next)
		b.active = focus.Call("getAttribute", "data-saved-key").String()
		b.focusID = focus.Get("id").String()
		b.render()
		focus.Call("scrollIntoView", map[string]any{"block": "nearest"})
		return true
	case "ArrowLeft", "ArrowRight":
		tab := target.Call("closest", "[role='tab']")
		if !tab.Truthy() || modified {
			return false
		}
		tabs := panel.Call("querySelectorAll", "[role='tab']")
		step := 1
		if key == "ArrowLeft" {
			step = -1
		}
		if dir := target.Call("closest", "[dir]"); dir.Truthy() && dir.Call("getAttribute", "dir").String() == "rtl" {
			step = -step
		}
		for i := 0; i < tabs.Length(); i++ {
			if tabs.Index(i).Equal(tab) {
				next := (i + step + tabs.Length()) % tabs.Length()
				stop()
				b.focusID = tabs.Index(next).Get("id").String()
				b.selectTab(tabs.Index(next).Call("getAttribute", "data-saved-tab").String())
				return true
			}
		}
		return false
	case "Enter":
		if item.Truthy() && item.Equal(target) && target.Call("getAttribute", "data-saved-action").String() == "open" && !modified {
			stop()
			target.Call("click")
			return true
		}
		return false
	}
	if modified || editable || !item.Truthy() {
		return false
	}
	press := func(action string) bool {
		if button := item.Call("querySelector", "[data-saved-action='"+action+"']"); button.Truthy() {
			stop()
			button.Call("click")
			return true
		}
		return false
	}
	switch {
	case strings.EqualFold(key, "d"):
		return press("done")
	case strings.EqualFold(key, "r"):
		return press("remind")
	case strings.EqualFold(key, "n"):
		return press("note")
	case key == "Delete":
		return press("remove")
	}
	return false
}

// chatsaveFocusStep moves focus along a list of buttons, wrapping at the ends.
func chatsaveFocusStep(list, current js.Value, step int) {
	n := list.Length()
	if n == 0 {
		return
	}
	at := -1
	for i := 0; i < n; i++ {
		if list.Index(i).Equal(current) {
			at = i
		}
	}
	list.Index((at + step + n) % n).Call("focus")
}
