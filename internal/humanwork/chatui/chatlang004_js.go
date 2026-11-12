//go:build js && wasm

package chatui

import (
	"context"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatlangOpenSettings opens the Chat preferences panel, where the reading
// language is, by pressing the gear in the Conversations heading, and opens the
// Reading language row's settings in it (CHATBUG-045). A panel that is already
// open stays open, and a row that is already expanded stays expanded.
func chatlangOpenSettings() {
	doc := js.Global().Get("document")
	if gear := doc.Call("querySelector", ".chatux002-gear"); gear.Truthy() {
		if gear.Call("getAttribute", "aria-expanded").String() != "true" {
			gear.Call("click")
		}
	}
	if change := doc.Call("querySelector", `[data-prefs-section="reading-languages"] .chat-prefs-change`); change.Truthy() {
		if change.Call("getAttribute", "aria-expanded").String() != "true" {
			change.Call("click")
		}
	}
}

// chatlangCorrect sends the writer's correction and, when the server accepted
// it, asks the page to read every message's selection again.
func chatlangCorrect(room, post string, revision uint64, language string, done func(error)) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := renderingSettingsClient().CorrectLanguage(ctx, room, post, revision, language)
		if err == nil {
			ui.PostAsync(func() {
				js.Global().Get("document").Call("dispatchEvent", js.Global().Get("Event").New("chat-reading-settings-changed"))
			})
		}
		done(err)
	}()
}

// chatlangFocusWhenDrawn moves focus to the element find returns once the page
// has drawn it. The editor opens on the frame after the press, so the lookup
// is retried for a few frames and gives up quietly if nothing ever draws.
func chatlangFocusWhenDrawn(find func(doc js.Value) js.Value) {
	var frame js.Func
	frames := 10
	frame = js.FuncOf(func(js.Value, []js.Value) any {
		if target := find(js.Global().Get("document")); target.Truthy() {
			target.Call("focus")
			frame.Release()
			return nil
		}
		frames--
		if frames <= 0 {
			frame.Release()
			return nil
		}
		js.Global().Call("requestAnimationFrame", frame)
		return nil
	})
	js.Global().Call("requestAnimationFrame", frame)
}

// chatlangFindByAttr returns the first element matching selector whose attribute
// name equals value. The message id is compared as text, never spliced into a
// selector.
func chatlangFindByAttr(doc js.Value, selector, name, value string) js.Value {
	nodes := doc.Call("querySelectorAll", selector)
	for i := 0; i < nodes.Get("length").Int(); i++ {
		if node := nodes.Index(i); node.Call("getAttribute", name).String() == value {
			return node
		}
	}
	return js.Value{}
}

// focusChatLanguageEditor puts focus in a message's language editor when it
// opens: on the current language choice, else the first choice.
func focusChatLanguageEditor(id string) {
	chatlangFocusWhenDrawn(func(doc js.Value) js.Value {
		group := chatlangFindByAttr(doc, "[data-chatlang-fix]", "data-chatlang-fix", id)
		if !group.Truthy() {
			return js.Value{}
		}
		if current := group.Call("querySelector", ".chatlang-fix-option[aria-pressed='true']"); current.Truthy() {
			return current
		}
		return group.Call("querySelector", ".chatlang-fix-option")
	})
}

// focusMessageAfterEdit moves focus back to a message after its language editor
// closes: to the message's More actions button, or, when that is gone, to the
// message row itself (made focusable for the purpose).
func focusMessageAfterEdit(id string) {
	chatlangFocusWhenDrawn(func(doc js.Value) js.Value {
		if more := chatlangFindByAttr(doc, "button[data-action='menu']", "data-id", id); more.Truthy() {
			return more
		}
		row := chatlangFindByAttr(doc, ".message[data-message-id],.thread-message[data-message-id]", "data-message-id", id)
		if row.Truthy() {
			row.Call("setAttribute", "tabindex", "-1")
		}
		return row
	})
}

// chatlangEventInEditor reports whether a key press came from inside an open
// language editor.
func chatlangEventInEditor(e ui.KeyboardEvent) bool {
	target := e.JSValue().Get("target")
	return target.Truthy() && target.Get("closest").Type() == js.TypeFunction && target.Call("closest", "[data-chatlang-fix]").Truthy()
}
