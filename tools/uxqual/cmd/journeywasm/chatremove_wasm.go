//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type chatremoveBrowser struct {
	mu                                sync.Mutex
	config                            journeyclient.Config
	locale                            string
	submit, input, key, click, resize js.Func
	// escape hears Escape before the workspace does (chatremove_escape.go).
	escape js.Func
	// pageOpened closes the page when the Saved panel or the search results
	// open in its place (CHATBUG-078).
	pageOpened, pageRestore js.Func
	disposed                bool
	generation              uint64
	opener                  js.Value
	// pageHref is the address of the Moderation page that is showing (with its
	// tab), so a decision can read it again; the openers are what focus returns to.
	pageHref                 string
	pageOpener, dialogOpener js.Value
	busy                     bool
	// pageSearched is set when the page is being read again for a search, so
	// the search box keeps the caret when the answer is drawn.
	pageSearched bool
	// dialogMessage is the message whose menu opened the dialog, and
	// dialogInThread whether that was in the thread pane (CHATBUG-085).
	dialogMessage  string
	dialogInThread bool
}

// configureChatremove binds delegated forms without adding hooks to an existing
// component. The composition root retains and calls the returned cleanup.
func configureChatremove(cfg journeyclient.Config, locale string) func() {
	b := &chatremoveBrowser{config: personaChatHTTPConfig(cfg), locale: locale}
	b.submit = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			b.handleSubmit(args[0])
		}
		return nil
	})
	b.input = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
			return nil
		}
		if target.Get("id").String() == "chatremove-search" {
			// The queue is filtered as the person types, from what is on the page.
			b.filterItems(target.Get("value").String())
			return nil
		}
		if target.Call("matches", "input[type=radio][name=reason]").Bool() {
			chatbug085ReasonChanged(target)
		}
		form := target.Call("closest", "form[data-chatremove]")
		if form.Truthy() && form.Call("getAttribute", "data-chatremove").String() == "apply" {
			form.Call("setAttribute", "data-chatremove", "preview")
			form.Call("removeAttribute", "data-confirmation")
			button := form.Call("querySelector", "button[type=submit]")
			if button.Truthy() {
				button.Set("disabled", false)
				button.Set("textContent", chatui.ModerationText(locale, "preview"))
			}
		}
		return nil
	})
	document := js.Global().Get("document")
	fields := document.Call("querySelectorAll", ".chatremove [data-chat-value]")
	for i := 0; i < fields.Length(); i++ {
		field := fields.Index(i)
		field.Set("value", field.Call("getAttribute", "data-chat-value").String())
	}
	b.click = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || b.disposed {
			return nil
		}
		event := args[0]
		target := event.Get("target")
		if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
			return nil
		}
		if target.Call("matches", "[data-chatremove-overlay=dialog]").Bool() {
			// A press on the backdrop, outside the dialog, closes it (CHATBUG-085).
			b.closeKind("dialog")
			return nil
		}
		if closer := target.Call("closest", "[data-chatremove-close]"); closer.Truthy() {
			event.Call("preventDefault")
			// The control closes the layer it is in: a dialog over the page leaves
			// the page showing.
			if layer := closer.Call("closest", "[data-chatremove-overlay]"); layer.Truthy() {
				b.closeKind(layer.Call("getAttribute", "data-chatremove-overlay").String())
			} else {
				b.closeTop()
			}
			return nil
		}
		if act := target.Call("closest", "[data-chatremove-act]"); act.Truthy() {
			event.Call("preventDefault")
			b.act(act)
			return nil
		}
		anchor := target.Call("closest", "[data-chatremove-open],a[href]")
		if !anchor.Truthy() {
			if b.overlay("page").Truthy() && !b.overlay("dialog").Truthy() && target.Call("closest", ".chat-rail").Truthy() {
				// Choosing a conversation leaves the page for it.
				b.closeKind("page")
			}
			return nil
		}
		href := domAttribute(anchor, "data-chatremove-open")
		if href == "" {
			href = domAttribute(anchor, "href")
		}
		if !strings.HasPrefix(href, "/api/chat/moderation/page") {
			if b.overlay("page").Truthy() && !b.overlay("dialog").Truthy() {
				// A link out of the page (a conversation) leaves it.
				b.closeKind("page")
			}
			return nil
		}
		if event.Get("ctrlKey").Bool() || event.Get("metaKey").Bool() || event.Get("shiftKey").Bool() || event.Get("button").Int() != 0 {
			return nil
		}
		event.Call("preventDefault")
		if chatmodIsDialog(href) {
			b.dialogFromMenu(anchor)
		}
		b.openOverlay(href, anchor)
		return nil
	})
	document.Call("addEventListener", "click", b.click)
	document.Call("addEventListener", "submit", b.submit)
	document.Call("addEventListener", "input", b.input)
	b.key = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || (!b.overlay("dialog").Truthy() && !b.overlay("page").Truthy()) {
			return nil
		}
		event := args[0]
		if event.Get("key").String() == "Escape" {
			event.Call("preventDefault")
			b.closeTop()
			return nil
		}
		dialog := b.overlay("dialog")
		if event.Get("key").String() != "Tab" || !dialog.Truthy() {
			return nil
		}
		controls := dialog.Call("querySelectorAll", "button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),a[href]")
		if controls.Length() == 0 {
			return nil
		}
		first, last := controls.Index(0), controls.Index(controls.Length()-1)
		active := document.Get("activeElement")
		backward := event.Get("shiftKey").Bool()
		if backward && active.Equal(first) {
			event.Call("preventDefault")
			last.Call("focus")
		}
		if !backward && active.Equal(last) {
			event.Call("preventDefault")
			first.Call("focus")
		}
		return nil
	})
	document.Call("addEventListener", "keydown", b.key)
	// Escape on a control of the dialog (a reason, the details box) reaches the
	// workspace's key handler before the document, and that handler stops it and
	// closes whatever is behind the dialog. This listener is ahead of it.
	b.escape = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || b.disposed || args[0].Get("key").String() != "Escape" {
			return nil
		}
		event := args[0]
		page := b.overlay("page")
		target := event.Get("target")
		inPage := page.Truthy() && target.Truthy() && target.Get("nodeType").Type() == js.TypeNumber && page.Call("contains", target).Bool()
		kind := chatremoveEscapeCloses(b.overlay("dialog").Truthy(), page.Truthy(), inPage, event.Get("isComposing").Truthy())
		if kind == "" {
			return nil
		}
		event.Call("preventDefault")
		event.Call("stopImmediatePropagation")
		b.closeKind(kind)
		return nil
	})
	js.Global().Call("addEventListener", "keydown", b.escape, true)
	b.resize = js.FuncOf(func(js.Value, []js.Value) any {
		b.placePage()
		return nil
	})
	js.Global().Call("addEventListener", "resize", b.resize)
	b.pageOpened = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && !b.disposed && chatPageOpenedKind(args[0]) != "moderation" {
			// The page that opened has focus; the opener must not take it back.
			b.pageOpener = js.Undefined()
			b.closeKind("page")
		}
		return nil
	})
	document.Call("addEventListener", chatPageOpenedEvent, b.pageOpened)
	// The address names a page (CHATBUG-052): the page shows when it is the
	// Moderation page and gives way to any other.
	b.pageRestore = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || b.disposed {
			return nil
		}
		showing := b.overlay("page").Truthy()
		switch {
		case chatPageOpenedKind(args[0]) == "moderation" && !showing:
			b.openOverlay(chatui.ModerationPageHref+"?locale="+b.locale, js.Undefined())
		case chatPageOpenedKind(args[0]) != "moderation" && showing:
			b.pageOpener = js.Undefined()
			b.closeKind("page")
		}
		return nil
	})
	document.Call("addEventListener", chatPageRestoreEvent, b.pageRestore)
	// CHATMOD-005: what the sidebar entry, the message menu and the removed
	// messages need to know about moderation is one read at load.
	go chatmod005Refresh(b.config, false)
	return func() {
		b.disposed = true
		b.closeKind("dialog")
		b.closeKind("page")
		document.Call("removeEventListener", "click", b.click)
		b.click.Release()
		document.Call("removeEventListener", "submit", b.submit)
		document.Call("removeEventListener", "input", b.input)
		document.Call("removeEventListener", "keydown", b.key)
		js.Global().Call("removeEventListener", "keydown", b.escape, true)
		b.escape.Release()
		js.Global().Call("removeEventListener", "resize", b.resize)
		document.Call("removeEventListener", chatPageOpenedEvent, b.pageOpened)
		b.pageOpened.Release()
		document.Call("removeEventListener", chatPageRestoreEvent, b.pageRestore)
		b.pageRestore.Release()
		b.submit.Release()
		b.input.Release()
		b.key.Release()
		b.resize.Release()
	}
}

func (b *chatremoveBrowser) handleSubmit(event js.Value) {
	if b.disposed {
		return
	}
	form := event.Get("target").Call("closest", "form[data-chatremove]")
	if !form.Truthy() {
		return
	}
	event.Call("preventDefault")
	if form.Call("getAttribute", "data-chatremove").String() == "filter" {
		// Typing hides the rows on the page that do not match. Enter asks the
		// server, which searches the whole queue and its history, and the page
		// is read again with the answer; the caret goes back to the box.
		if field := form.Call("querySelector", "#chatremove-search"); field.Truthy() && b.pageHref != "" {
			b.pageSearched = true
			b.openOverlay(chatmod005SearchHref(b.pageHref, field.Get("value").String()), js.Undefined())
		}
		return
	}
	if form.Call("getAttribute", "data-chatremove").String() == "role" {
		// CHATMOD-005: the Permissions tab is read again with a row for the role
		// that was typed. Nothing is stored by this.
		if field := form.Call("querySelector", "[name=role]"); field.Truthy() && b.pageHref != "" {
			b.openOverlay(chatmod005RoleHref(b.pageHref, field.Get("value").String()), js.Undefined())
		}
		return
	}
	b.mu.Lock()
	if b.busy {
		b.mu.Unlock()
		return
	}
	b.busy = true
	b.mu.Unlock()
	action := form.Call("getAttribute", "data-chatremove").String()
	values := map[string]string{}
	for _, name := range []string{"reason", "note", "query", "author", "from", "until"} {
		field := form.Call("querySelector", "[name="+name+"]")
		if field.Truthy() {
			values[name] = field.Get("value").String()
		}
	}
	// The reasons are radio buttons: the value is the one that is chosen, and
	// nothing is chosen until the person chooses.
	if radios := form.Call("querySelectorAll", "input[type=radio][name=reason]"); radios.Length() > 0 {
		values["reason"] = ""
		if chosen := form.Call("querySelector", "input[type=radio][name=reason]:checked"); chosen.Truthy() {
			values["reason"] = chosen.Get("value").String()
		}
	}
	for name, attribute := range map[string]string{"tenant": "host-tenant", "conversation": "conversation-id", "post": "post-id", "case": "case-id", "removal_action": "removal-action", "confirmation": "confirmation", "confirmed_count": "confirmed-count"} {
		value := form.Call("getAttribute", "data-"+attribute)
		if !value.IsNull() {
			values[name] = value.String()
		}
	}
	if author := form.Call("querySelector", "[name=author] option:checked,input[name=author]"); author.Truthy() {
		home := author.Call("getAttribute", "data-home-tenant")
		if !home.IsNull() {
			values["author_home"] = home.String()
		}
	}
	if submitter := event.Get("submitter"); submitter.Truthy() {
		values["decision"] = submitter.Get("value").String()
	}
	var ids []string
	selected := form.Call("querySelectorAll", "input[name=post]:checked")
	for i := 0; i < selected.Length(); i++ {
		ids = append(ids, selected.Index(i).Get("value").String())
	}
	if len(ids) == 0 && values["post"] != "" {
		ids = append(ids, values["post"])
	}
	if values["decision"] == "message_author" && strings.TrimSpace(values["note"]) == "" {
		// Message the author sends the note itself; there is nothing to send without it.
		b.finish(form, errChatmod005NoteRequired)
		return
	}
	input, err := chatremoveFormInput(action, values, ids, time.Local)
	if err != nil {
		b.finish(form, err)
		return
	}
	if action == "search" {
		action = ""
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var reply []byte
		var e error
		if action == "quick" {
			reply, e = chatmod004Quick(ctx, b.config, input)
		} else {
			reply, e = chatremoveRequest(ctx, http.DefaultClient, b.config, action, input.Query, input)
		}
		ui.PostAsync(func() {
			if b.disposed || !form.Get("isConnected").Bool() {
				return
			}
			defer b.finish(form, e)
			if e != nil {
				return
			}
			if action == "preview" {
				var preview chat.RemovalPreview
				if json.Unmarshal(reply, &preview) != nil {
					return
				}
				form.Call("setAttribute", "data-chatremove", "apply")
				form.Call("setAttribute", "data-confirmation", preview.Confirmation)
				form.Call("setAttribute", "data-confirmed-count", strconv.Itoa(preview.Count))
				status := form.Call("querySelector", "[data-chatremove-count]")
				if !status.Truthy() {
					status = js.Global().Get("document").Call("createElement", "p")
					status.Call("setAttribute", "data-chatremove-count", "")
					status.Call("setAttribute", "role", "status")
					form.Call("appendChild", status)
				}
				status.Set("textContent", chatui.ModerationCountText(b.locale, preview.Count))
				button := form.Call("querySelector", "button[type=submit]")
				button.Set("disabled", preview.Count == 0)
				key := "confirm"
				if input.Removal.Action == "restore" {
					key = "confirm_restore"
				}
				button.Set("textContent", chatui.ModerationText(b.locale, key))
				return
			}
			if action == "report" {
				form.Call("setAttribute", "role", "status")
				form.Set("textContent", chatui.ModerationText(b.locale, "sent"))
				return
			}
			if action == "appeal" {
				form.Call("setAttribute", "role", "status")
				form.Set("textContent", chatui.ModerationText(b.locale, "appeal_sent"))
				go chatmod005Refresh(b.config, false)
				return
			}
			if action == "quick" || action == "apply" {
				// One decision on the message (or the confirmed count of several):
				// the dialog closes and the chat shows the message as readers see it.
				b.closeKind("dialog")
				go chatmod005Refresh(b.config, true)
				return
			}
			if action == "resolve" {
				// The decision closes its dialog and the queue is read again.
				b.closeKind("dialog")
				b.reloadPage()
				go chatmod005Refresh(b.config, true)
				return
			}
		})
	}()
}

func (b *chatremoveBrowser) finish(form js.Value, err error) {
	b.mu.Lock()
	b.busy = false
	b.mu.Unlock()
	if err == nil {
		return
	}
	if chatremoveErrorKey(err) == "conflict" && form.Call("getAttribute", "data-chatremove").String() == "apply" {
		form.Call("setAttribute", "data-chatremove", "preview")
		form.Call("removeAttribute", "data-confirmation")
		button := form.Call("querySelector", "button[type=submit]")
		if button.Truthy() {
			button.Set("disabled", false)
			button.Set("textContent", chatui.ModerationText(b.locale, "preview"))
		}
	}
	status := form.Call("querySelector", "[role=alert]")
	if !status.Truthy() {
		status = js.Global().Get("document").Call("createElement", "p")
		status.Call("setAttribute", "role", "alert")
		status.Call("setAttribute", "id", "chatremove-form-error")
		form.Call("appendChild", status)
	}
	form.Call("setAttribute", "aria-describedby", status.Get("id").String())
	status.Set("textContent", chatui.ModerationText(b.locale, chatremoveErrorKey(err)))
}
