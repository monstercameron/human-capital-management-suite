//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// The event closures own this state. Nothing private is put in localStorage,
// BroadcastChannel, a shared event stream, or a process-wide registry.
type chatsaveBrowser struct {
	cfg                   journeyclient.Config
	page                  chat.SavedPage
	tab, query, errorCode string
	disposed              bool
	refreshPending        bool
	refresh               chatsaveRefresh
	busy                  bool
	lastMarkup            string
	pending               []chatsavePendingCommand
	// listDown is set only when the list itself cannot be read; a refused save
	// or unsave must never switch the whole Saved row off.
	listDown bool
	// commandFailed keeps a refused command's message until the person acts again.
	commandFailed bool
	// failedAction is which command was refused, so the message names it.
	failedAction string
	// localEpoch counts local, not yet confirmed changes so a list read that
	// started before one cannot put the old count back.
	localEpoch uint64
	// retryDone receives the outcome of the list read a retry started
	// (CHATUX-012), so the retry loop knows whether to wait and try again.
	retryDone chan bool
	// CHATSAVE-002: the panel's own view state. expanded holds the posts shown
	// in full; active is the item last used from the keyboard; editingNote and
	// reminderMenu name the item whose note is being written or whose reminder
	// menu is open (pickingDate shows the date field in that menu); undo is the
	// Undo line, which removes itself after six seconds; focusID is the element
	// the next draw puts focus on.
	expanded                          map[string]bool
	active, editingNote, reminderMenu string
	pickingDate                       bool
	undo                              *chatsaveUndo
	undoToken                         int
	focusID                           string
	// pointer is true while the last press was a click rather than a key.
	pointer bool
	// loaded is true once a read of the list has landed, so a command in flight
	// on an empty list is never drawn as the list loading.
	loaded bool
	// tabSettled is true once the panel has chosen its tab for this opening
	// (CHATBUG-091): the first tab with items, or the one the person pressed.
	tabSettled bool
}

// settleTab opens the panel on the first tab that holds items, once the list has
// been read, and then leaves the tab alone until the panel is opened again.
func (b *chatsaveBrowser) settleTab() {
	if b.tabSettled || !b.loaded {
		return
	}
	b.tabSettled = true
	todo, done, all := chatsaveCounts(b.page)
	b.tab = chatui.ChatBug091FirstTab(todo, done, all)
}

// chatsaveUndo is what the Undo line puts back: a ticked-off item is reopened,
// a removed one is saved again with its note, reminder and state.
type chatsaveUndo struct {
	kind string
	host string
	item chat.SavedItem
}

type chatsavePendingCommand struct {
	host    string
	command chatsaveCommand
}

func (b *chatsaveBrowser) nextCommand() bool {
	if len(b.pending) == 0 {
		return false
	}
	next := b.pending[0]
	b.pending = b.pending[1:]
	b.command(next.host, next.command)
	return true
}

func installSavedMessages(cfg journeyclient.Config) func() {
	cfg = personaChatHTTPConfig(cfg)
	b := &chatsaveBrowser{cfg: cfg, tab: "todo"}
	doc := js.Global().Get("document")
	click := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && !b.disposed {
			b.click(args[0])
		}
		return nil
	})
	submit := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && !b.disposed {
			b.submit(args[0])
		}
		return nil
	})
	key := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && !b.disposed {
			b.key(args[0])
		}
		return nil
	})
	// The search box filters as the person types, like the other panels' filters.
	input := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && !b.disposed {
			if target := args[0].Get("target"); target.Truthy() && target.Get("id").String() == "chatsave-search" {
				b.query = target.Get("value").String()
				b.render()
			}
		}
		return nil
	})
	// The panel stays docked under the application header as the window changes.
	resize := js.FuncOf(func(js.Value, []js.Value) any {
		if panel := js.Global().Get("document").Call("getElementById", "chatsave-list"); !b.disposed && panel.Truthy() && !panel.Get("hidden").Bool() {
			chatui.PositionSavedMessagesPanel(panel)
			b.measure(panel)
			b.placeMenu(panel)
		}
		return nil
	})
	js.Global().Call("addEventListener", "resize", resize)
	changed := js.FuncOf(func(js.Value, []js.Value) any {
		if !b.disposed {
			b.sync()
		}
		return nil
	})
	doc.Call("addEventListener", "click", click, true)
	doc.Call("addEventListener", "submit", submit, true)
	doc.Call("addEventListener", "keydown", key, true)
	doc.Call("addEventListener", "input", input, true)
	doc.Call("addEventListener", "chat-saved-changed", changed)
	// Moderation and the search results take the place of the panel.
	pageOpened := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && !b.disposed && chatPageOpenedKind(args[0]) != "saved" {
			if panel := b.panel(); panel.Truthy() && !panel.Get("hidden").Bool() {
				b.hide(false)
			}
		}
		return nil
	})
	doc.Call("addEventListener", chatPageOpenedEvent, pageOpened)
	// The address names a page (CHATBUG-052): the panel shows when it is the
	// Saved panel and gives way to any other.
	pageRestore := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 || b.disposed {
			return nil
		}
		panel := b.panel()
		if !panel.Truthy() {
			return nil
		}
		open := !panel.Get("hidden").Bool()
		if chatPageOpenedKind(args[0]) == "saved" && !open {
			b.open()
		} else if chatPageOpenedKind(args[0]) != "saved" && open {
			b.hide(false)
		}
		return nil
	})
	doc.Call("addEventListener", chatPageRestoreEvent, pageRestore)
	observerCallback := js.FuncOf(func(js.Value, []js.Value) any {
		if !b.disposed {
			b.marks()
			b.render()
		}
		return nil
	})
	observer := js.Global().Get("MutationObserver").New(observerCallback)
	observer.Call("observe", doc.Get("body"), map[string]any{"childList": true, "subtree": true})
	b.sync()
	return func() {
		b.disposed = true
		observer.Call("disconnect")
		doc.Call("removeEventListener", "click", click, true)
		doc.Call("removeEventListener", "submit", submit, true)
		doc.Call("removeEventListener", "keydown", key, true)
		doc.Call("removeEventListener", "input", input, true)
		js.Global().Call("removeEventListener", "resize", resize)
		input.Release()
		resize.Release()
		doc.Call("removeEventListener", "chat-saved-changed", changed)
		doc.Call("removeEventListener", chatPageOpenedEvent, pageOpened)
		pageOpened.Release()
		doc.Call("removeEventListener", chatPageRestoreEvent, pageRestore)
		pageRestore.Release()
		click.Release()
		submit.Release()
		key.Release()
		changed.Release()
		observerCallback.Release()
		b.page = chat.SavedPage{}
		b.pending = nil
	}
}

func (b *chatsaveBrowser) locale() string {
	if mount := js.Global().Get("document").Call("querySelector", "[data-saved-mount]"); mount.Truthy() {
		if locale := mount.Call("getAttribute", "data-saved-locale").String(); locale != "" {
			return locale
		}
	}
	return "en-US"
}

func (b *chatsaveBrowser) sync() {
	if b.disposed || b.listDown {
		return
	}
	if b.busy {
		b.refreshPending = true
		return
	}
	if !b.refresh.ready(time.Now()) {
		return
	}
	b.refreshPending = false
	b.busy = true
	cfg := b.cfg
	epoch := b.localEpoch
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		all := chat.SavedPage{Items: []chat.SavedItem{}}
		cursor := ""
		var err error
		for len(all.Items) <= chat.SavedLimit {
			var page chat.SavedPage
			page, err = chatsaveRequest(ctx, http.DefaultClient, cfg, cfg.Tenant, "all", cursor, "", nil)
			if err != nil {
				break
			}
			all.Items = append(all.Items, page.Items...)
			if page.NextCursor == "" {
				break
			}
			if page.NextCursor == cursor {
				err = chatsaveError("unavailable")
				break
			}
			cursor = page.NextCursor
		}
		if len(all.Items) > chat.SavedLimit {
			err = chatsaveError("unavailable")
		}
		ui.PostAsync(func() {
			if b.disposed {
				return
			}
			b.refresh.complete(time.Now(), err)
			b.busy = false
			if b.retryDone != nil {
				b.retryDone <- err == nil
				b.retryDone = nil
			}
			if err != nil {
				// A failed authority read clears old text instead of keeping a stale list.
				b.page = chat.SavedPage{}
				b.loaded = false
				b.errorCode = chatsaveErrorCode(err)
				b.listDown = b.errorCode == "unavailable"
				if b.listDown {
					chatux012RetrySaved(b)
				}
			} else if epoch != b.localEpoch {
				// A change was made while this read was in flight: keep the
				// local view and read again once the commands have landed.
				b.refreshPending = true
			} else {
				b.page = all
				b.loaded = true
				b.listDown = false
				if !b.commandFailed && b.errorCode != "saved_limit" && b.errorCode != "invalid_argument" {
					b.errorCode = ""
				}
			}
			b.marks()
			b.render()
			if !b.nextCommand() && b.refreshPending {
				b.sync()
			}
		})
	}()
}

func (b *chatsaveBrowser) view() chatui.SavedMessagesView {
	cfg := b.cfg
	cfg.Locale = b.locale()
	b.settleTab()
	page := chat.SavedPage{Items: []chat.SavedItem{}}
	query := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(b.query), "in: saved")))
	for _, item := range b.page.Items {
		if !chatsaveInTab(item, b.tab) {
			continue
		}
		text := item.Note
		if item.Availability == "readable" && item.Post != nil {
			text += "\n" + item.Post.Body
		}
		if query != "" && !strings.Contains(strings.ToLower(text), query) {
			continue
		}
		page.Items = append(page.Items, item)
	}
	snapshot := chatBrowser.snapshot()
	rows := chatsaveRows(page, cfg, snapshot)
	listError, actionError := chatsaveActionFailure(b.errorCode, b.commandFailed)
	todo, done, all := chatsaveCounts(b.page)
	undo := ""
	if b.undo != nil {
		undo = b.undo.kind
	}
	// Times are the viewer's: Go's local zone in the browser is the page's own.
	return chatui.SavedMessagesView{Locale: cfg.Locale, Tab: b.tab, Query: b.query, Rows: rows, Error: listError, ActionError: actionError, Action: b.failedAction, DocPreviews: snapshot.DocPreviews, EmbedOrigin: snapshot.EmbedOrigin, Loading: b.busy && !b.loaded,
		Model: snapshot, Now: time.Now(), TodoCount: todo, DoneCount: done, AllCount: all, Expanded: b.expanded, Active: b.active, EditingNote: b.editingNote, ReminderMenu: b.reminderMenu, PickingDate: b.pickingDate, Undo: undo}
}

func (b *chatsaveBrowser) render() {
	doc := js.Global().Get("document")
	mount := doc.Call("querySelector", "[data-saved-mount]")
	if !mount.Truthy() {
		return
	}
	markup, err := ui.RenderToString(chatui.RenderSavedMessages(b.view()))
	if err != nil {
		return
	}
	if markup == b.lastMarkup && mount.Call("querySelector", ".chatsave-content").Truthy() && mount.Call("getAttribute", "data-saved-rendered").String() == "true" {
		b.applyFocus(mount)
		return
	}
	// Keep what the person has typed and where they are in it across a redraw:
	// the search, a note being written, a date being picked.
	values := map[string]string{}
	starts := map[string][2]int{}
	fields := mount.Call("querySelectorAll", "input,textarea")
	for i := 0; i < fields.Length(); i++ {
		field := fields.Index(i)
		if id := field.Get("id").String(); id != "" {
			values[id] = field.Get("value").String()
			if field.Get("selectionStart").Type() == js.TypeNumber {
				starts[id] = [2]int{field.Get("selectionStart").Int(), field.Get("selectionEnd").Int()}
			}
		}
	}
	keep := ""
	if active := doc.Get("activeElement"); b.focusID == "" && active.Truthy() && mount.Call("contains", active).Bool() {
		keep = active.Get("id").String()
	}
	scroll := mount.Get("scrollTop")
	mount.Set("innerHTML", markup)
	mount.Call("setAttribute", "data-saved-rendered", "true")
	b.lastMarkup = markup
	fields = mount.Call("querySelectorAll", "input,textarea")
	for i := 0; i < fields.Length(); i++ {
		field := fields.Index(i)
		id := field.Get("id").String()
		if value, ok := values[id]; ok {
			field.Set("value", value)
			if at, ok := starts[id]; ok && field.Get("setSelectionRange").Type() == js.TypeFunction && field.Get("type").String() != "datetime-local" {
				field.Call("setSelectionRange", at[0], at[1])
			}
		} else if value := field.Call("getAttribute", "data-chat-value"); value.Type() == js.TypeString {
			field.Set("value", value.String())
		}
	}
	mount.Set("scrollTop", scroll)
	b.measure(mount)
	b.placeMenu(mount)
	if keep != "" {
		// Focus the person already had stays where it was, whatever the last press was.
		if focus := doc.Call("getElementById", keep); focus.Truthy() && mount.Call("contains", focus).Bool() {
			focus.Call("focus", map[string]any{"preventScroll": true})
		}
	}
	b.applyFocus(mount)
}

// applyFocus puts focus where the last action asked for it, once.
func (b *chatsaveBrowser) applyFocus(mount js.Value) {
	id := b.focusID
	b.focusID = ""
	if id == "" {
		return
	}
	// After a click, focus goes only where the person is about to type or choose
	// (the note field, the date field, the reminder menu); it is never parked on
	// an item or a button the pointer is not over.
	if b.pointer && !strings.HasPrefix(id, "chatsave-note-field-") && !strings.HasPrefix(id, "chatsave-due-") && !strings.HasPrefix(id, "chatsave-preset-") {
		return
	}
	focus := js.Global().Get("document").Call("getElementById", id)
	if !focus.Truthy() || !mount.Call("contains", focus).Bool() {
		return
	}
	if active := js.Global().Get("document").Get("activeElement"); active.Truthy() && active.Equal(focus) {
		return
	}
	focus.Call("focus")
	if focus.Get("select").Type() == js.TypeFunction && focus.Get("id").String() != "chatsave-search" && strings.HasPrefix(id, "chatsave-note-field-") {
		focus.Call("select")
	}
}

func (b *chatsaveBrowser) marks() {
	if row := js.Global().Get("document").Call("querySelector", "[data-saved-action=toggle]"); row.Truthy() {
		row.Set("disabled", b.listDown)
		if b.listDown {
			row.Call("setAttribute", "title", chatui.ChatFeatureUnavailable(b.locale()))
		}
	}
	// CHATBUG-041: the sidebar's number is the count of items still to do from a
	// whole read of the list, and nothing until that read has landed; a list
	// changed locally before then is a partial list and is never counted.
	count, known := chatsaveOpenCount(b.page, b.loaded)
	bodies := chatsaveBodies(b.page)
	bodiesChanged := false
	chatBrowser.mutate(func(m *chatui.Model) {
		if known {
			m.SavedOpenCount = count
		}
		bodiesChanged = !slices.Equal(m.SavedBodies, bodies)
		m.SavedBodies = bodies
	})
	if bodiesChanged {
		// A saved message may refer to a document; read the titles it needs.
		ui.PostAsync(func() { resolveVisibleChatDocs(journeyclient.Config{}) })
	}
	badge := js.Global().Get("document").Call("querySelector", "[data-saved-count]")
	if badge.Truthy() && !known {
		badge.Set("hidden", true)
	} else if badge.Truthy() {
		label := chatBrowser.snapshot().Number
		text := strconv.Itoa(count)
		if label != nil {
			text = label(count)
		}
		if badge.Get("textContent").String() != text {
			badge.Set("textContent", text)
		}
		badge.Set("hidden", count == 0)
	}
	buttons := js.Global().Get("document").Call("querySelectorAll", "button[data-saved-action='save']")
	for i := 0; i < buttons.Length(); i++ {
		button := buttons.Index(i)
		button.Set("disabled", b.listDown || button.Call("getAttribute", "data-saved-disabled").String() == "true")
		if b.listDown {
			button.Call("setAttribute", "title", chatui.ChatFeatureUnavailable(b.locale()))
		}
		saved := chatsaveIsSaved(b.page, b.cfg, button.Call("getAttribute", "data-saved-host").String(), button.Call("getAttribute", "data-saved-conversation").String(), button.Call("getAttribute", "data-saved-post").String())
		pressed := "false"
		label := button.Call("getAttribute", "data-saved-label").String()
		if saved {
			pressed = "true"
			label = button.Call("getAttribute", "data-saved-remove").String()
		}
		if button.Call("getAttribute", "aria-pressed").String() != pressed {
			button.Call("setAttribute", "aria-pressed", pressed)
			button.Call("setAttribute", "aria-label", label)
			if !b.listDown {
				button.Call("setAttribute", "title", label)
			}
			if text := button.Call("querySelector", "[data-saved-button-label]"); text.Truthy() {
				text.Set("textContent", label)
			}
		}
	}
}

func (b *chatsaveBrowser) click(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	// A press anywhere but the reminder menu and its bell closes the menu.
	// A press with a pointer (a click has a detail; a key press that activates a
	// button does not) never leaves an item marked as the keyboard's, and never
	// moves focus onto an item the pointer is not over.
	b.pointer = event.Get("detail").Int() > 0
	if b.pointer {
		b.active = ""
	}
	if b.reminderMenu != "" && !target.Call("closest", "[data-saved-menu],[data-saved-action='remind'],[data-saved-action='remind-pick'],[data-saved-action='preset']").Truthy() {
		b.reminderMenu, b.pickingDate = "", false
		b.render()
	}
	if tab := target.Call("closest", "[data-saved-tab]"); tab.Truthy() {
		event.Call("preventDefault")
		event.Call("stopPropagation")
		b.selectTab(tab.Call("getAttribute", "data-saved-tab").String())
		return
	}
	button := target.Call("closest", "[data-saved-action]")
	if !button.Truthy() {
		return
	}
	if button.Call("hasAttribute", "data-saved-item").Bool() {
		// The whole item opens the message, except where the press belongs to
		// something inside it (a link, a mention, a button) or ends a selection.
		if inner := target.Call("closest", "a,button,input,textarea,label,select,[data-action]"); inner.Truthy() && !inner.Equal(button) {
			return
		}
		if selection := js.Global().Call("getSelection"); selection.Truthy() && selection.Call("toString").String() != "" {
			return
		}
	}
	event.Call("preventDefault")
	event.Call("stopPropagation")
	action := button.Call("getAttribute", "data-saved-action").String()
	host := button.Call("getAttribute", "data-saved-host").String()
	conversationID := button.Call("getAttribute", "data-saved-conversation").String()
	postID := button.Call("getAttribute", "data-saved-post").String()
	key := chatui.SavedItemKey(host, conversationID, postID)
	switch action {
	case "toggle":
		panel := b.panel()
		if panel.Truthy() && panel.Get("hidden").Bool() {
			b.open()
		} else {
			b.close()
		}
		return
	case "close":
		b.close()
		return
	case "retry":
		b.refresh = chatsaveRefresh{}
		b.errorCode, b.commandFailed, b.listDown = "", false, false
		b.sync()
		return
	case "more":
		b.sync()
		return
	case "expand":
		if b.expanded == nil {
			b.expanded = map[string]bool{}
		}
		b.expanded[postID] = !b.expanded[postID]
		b.focusID = "chatsave-expand-" + postID
		b.render()
		return
	case "open":
		b.active = ""
		for _, item := range b.page.Items {
			if item.ConversationID == conversationID && item.PostID == postID && item.Availability == "readable" && item.Post != nil {
				if callback := chatBrowser.snapshot().Callbacks.OpenSearchMessage; callback != nil {
					// The panel stays open beside the conversation; on a phone it is
					// a page of its own and gives way to the message.
					if chatsaveNarrow() {
						b.close()
					}
					callback(conversationID, postID, item.Post.Sequence)
				}
				return
			}
		}
		return
	case "open-channel":
		if callback := chatBrowser.snapshot().Callbacks.SelectConversation; callback != nil {
			if chatsaveNarrow() {
				b.close()
			}
			callback(conversationID)
		}
		return
	case "undo":
		b.runUndo()
		return
	case "remind":
		if b.reminderMenu == key {
			b.closeMenu(true)
			return
		}
		b.reminderMenu, b.pickingDate, b.editingNote = key, false, ""
		b.focusID = "chatsave-preset-hour-" + postID
		b.render()
		return
	case "remind-pick":
		b.pickingDate = true
		b.focusID = "chatsave-due-" + postID
		b.render()
		return
	case "preset":
		if at, ok := chatui.SavedReminderAt(button.Call("getAttribute", "data-saved-preset").String(), time.Now()); ok {
			b.dueCommand(host, conversationID, postID, &at)
		}
		return
	case "clear-due":
		b.dueCommand(host, conversationID, postID, nil)
		return
	case "note":
		b.editingNote, b.reminderMenu, b.pickingDate = key, "", false
		b.focusID = "chatsave-note-field-" + postID
		b.render()
		return
	case "done", "remove":
		// The item leaves the list at once; the Undo line puts it back.
		b.focusAfterRemoval(button.Call("closest", "[data-saved-item]"))
		if item, ok := chatsaveFind(b.page, host, conversationID, postID); ok {
			b.startUndo(action, host, item)
		}
	case "reopen":
		b.undo = nil
		b.undoToken++
		b.focusID = "chatsave-tab-" + b.tab
	}
	command := chatsaveCommand{Action: action, ConversationID: conversationID, PostID: postID}
	if action == "save" {
		// The list, not the button's pressed state, says whether this is a save
		// or an unsave: the button is redrawn with every hover.
		command = chatsaveHoverCommand(b.page, b.cfg, host, command.ConversationID, command.PostID)
	}
	if b.commandFailed {
		b.errorCode = ""
	}
	b.commandFailed = false
	b.applyLocal(host, command)
	b.command(host, command)
}

// applyLocal shows the result of a state change at once, in the count, the
// hover-bar bookmark and the list, before the service has answered. The read
// that follows every command replaces it with what the service holds.
func (b *chatsaveBrowser) applyLocal(host string, command chatsaveCommand) {
	switch command.Action {
	case "save", "remove", "done", "reopen", "due", "note":
	default:
		return
	}
	var add *chat.SavedItem
	if command.Action == "save" {
		add = chatsaveOptimisticItem(b.cfg, host, command, chatBrowser.snapshot(), time.Now())
	}
	b.page = chatsaveApplyLocal(b.page, host, command, add)
	b.localEpoch++
	b.marks()
	b.render()
}

func (b *chatsaveBrowser) submit(event js.Value) {
	form := event.Get("target")
	if !form.Truthy() || form.Get("matches").Type() != js.TypeFunction {
		return
	}
	if form.Call("matches", "[data-saved-search]").Bool() {
		event.Call("preventDefault")
		event.Call("stopPropagation")
		b.query = form.Call("querySelector", "input").Get("value").String()
		b.render()
		return
	}
	if !form.Call("matches", "[data-saved-form]").Bool() {
		return
	}
	event.Call("preventDefault")
	event.Call("stopPropagation")
	host := form.Call("getAttribute", "data-saved-host").String()
	conversationID := form.Call("getAttribute", "data-saved-conversation").String()
	postID := form.Call("getAttribute", "data-saved-post").String()
	value := form.Call("querySelector", "input").Get("value").String()
	if form.Call("getAttribute", "data-saved-form").String() == "note" {
		command := chatsaveCommand{Action: "note", ConversationID: conversationID, PostID: postID, Note: &value}
		b.editingNote = ""
		b.focusID = "chatsave-note-" + postID
		if b.commandFailed {
			b.errorCode = ""
		}
		b.commandFailed = false
		b.applyLocal(host, command)
		b.command(host, command)
		return
	}
	if value == "" {
		return
	}
	date := js.Global().Get("Date").New(value)
	if js.Global().Call("isNaN", date.Call("getTime")).Bool() {
		b.errorCode = "invalid_argument"
		b.render()
		return
	}
	due, err := time.Parse(time.RFC3339Nano, date.Call("toISOString").String())
	if err != nil {
		return
	}
	b.dueCommand(host, conversationID, postID, &due)
}

func (b *chatsaveBrowser) command(host string, command chatsaveCommand) {
	if b.disposed {
		return
	}
	if b.busy {
		b.pending = append(b.pending, chatsavePendingCommand{host, command})
		return
	}
	b.busy = true
	cfg := b.cfg
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_, err := chatsaveRequest(ctx, http.DefaultClient, cfg, host, "all", "", "", &command)
		ui.PostAsync(func() {
			if b.disposed {
				return
			}
			b.refresh = chatsaveRefresh{}
			b.busy = false
			if err != nil {
				code := chatsaveErrorCode(err)
				panel := js.Global().Get("document").Call("getElementById", "chatsave-list")
				if panel.Truthy() && !panel.Get("hidden").Bool() {
					// The panel is open: the refused change is reported in it, and
					// the list stays on screen.
					b.errorCode, b.commandFailed, b.failedAction = code, true, command.Action
					b.render()
				} else {
					// A press on a message's own bookmark does not open the panel to
					// report itself; the notice names what failed.
					b.errorCode, b.commandFailed, b.failedAction = "", false, ""
					noteChatAction(chatui.SavedActionFailedText(b.locale(), command.Action))
				}
			} else {
				b.errorCode = ""
			}
			if !b.nextCommand() {
				b.sync()
			}
		})
	}()
}

func (b *chatsaveBrowser) close() { b.hide(true) }

// hide takes the panel away. A person closing it returns to the row that opened
// it; a page that opens in its place (Moderation, search results) keeps focus.
func (b *chatsaveBrowser) hide(refocus bool) {
	b.reminderMenu, b.editingNote, b.pickingDate, b.active = "", "", false, ""
	doc := js.Global().Get("document")
	if panel := doc.Call("getElementById", "chatsave-list"); panel.Truthy() {
		panel.Set("hidden", true)
		if panel.Get("hidePopover").Type() == js.TypeFunction && panel.Call("matches", ":popover-open").Bool() {
			panel.Call("hidePopover")
		}
	}
	if button := doc.Call("querySelector", "[data-saved-action=toggle]"); button.Truthy() {
		button.Call("setAttribute", "aria-expanded", "false")
		if refocus {
			button.Call("focus")
		}
	}
	chatPageSync()
}

func (b *chatsaveBrowser) key(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	if panel := b.panel(); panel.Truthy() && !panel.Get("hidden").Bool() && panel.Call("contains", target).Bool() && b.panelKey(event, target) {
		return
	}
	if event.Get("key").String() == "Escape" && chatui.ChatLayerIsTop(js.Global().Get("document").Call("querySelector", ".chatsave-panel:not([hidden])")) {
		event.Call("preventDefault")
		event.Call("stopPropagation")
		b.close()
		return
	}
	editable := target.Call("closest", "input,textarea,[contenteditable='true']").Truthy()
	if event.Get("isComposing").Truthy() || !chatsaveShortcut(event.Get("key").String(), event.Get("altKey").Bool(), event.Get("shiftKey").Bool(), event.Get("ctrlKey").Bool(), event.Get("metaKey").Bool(), editable, event.Get("repeat").Bool()) {
		return
	}
	row := target.Call("closest", ".message,.thread-message,.thread-root")
	if !row.Truthy() {
		return
	}
	button := row.Call("querySelector", "[data-saved-action='save']")
	if !button.Truthy() {
		return
	}
	event.Call("preventDefault")
	event.Call("stopPropagation")
	button.Call("click")
}

func (b *chatsaveBrowser) open() {
	doc := js.Global().Get("document")
	panel := doc.Call("getElementById", "chatsave-list")
	if !panel.Truthy() || b.listDown {
		return
	}
	announceChatPage("saved")
	panel.Set("hidden", false)
	// CHATBUG-091: every opening starts on the first tab that has items.
	b.tabSettled = false
	b.settleTab()
	chatui.PositionSavedMessagesPanel(panel)
	if opener := doc.Call("querySelector", "[data-saved-action=toggle]"); opener.Truthy() {
		chatui.OpenSavedMessagesLayer(panel, opener)
	}
	if button := doc.Call("querySelector", "[data-saved-action=toggle]"); button.Truthy() {
		button.Call("setAttribute", "aria-expanded", "true")
	}
	b.focusID = "chatsave-tab-" + b.tab
	b.render()
	b.measure(panel)
	b.applyFocus(panel)
	chatPageSync()
}
