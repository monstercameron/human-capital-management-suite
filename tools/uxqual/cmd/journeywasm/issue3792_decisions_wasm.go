//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3792: the browser half of /decide. It owns the Decisions dialog (drawn
// over the page, with its own focus handling), reads a conversation's decisions,
// records and replaces them, and tells the model whether /decide and the
// Decisions row of Conversation details can be offered.

type decisionsBrowser struct {
	mu       sync.Mutex
	cfg      journeyclient.Config
	disposed bool
	// The decisions of loadedConv as the server last gave them to this reader.
	// loadedOK is true once that read succeeded; failedAt rate-limits a retry.
	items      []decisionsJSON
	canRecord  bool
	loadedConv string
	loadedOK   bool
	loading    string
	failedConv string
	failedAt   time.Time
	// The dialog.
	open           bool
	view           chatui.DecisionsDialogView
	conversationID string
	parent         string
	sourcePost     string
	key            string
	returnFocus    string
	generation     uint64
	click, submit  js.Func
	keydown, input js.Func
}

var decisionsActive *decisionsBrowser

func installDecisions(cfg journeyclient.Config) func() {
	cfg = personaChatHTTPConfig(cfg)
	b := &decisionsBrowser{cfg: cfg}
	decisionsActive = b
	doc := js.Global().Get("document")
	wrap := func(fn func(js.Value)) js.Func {
		return js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 && !b.disposed {
				fn(args[0])
			}
			return nil
		})
	}
	b.click, b.submit, b.keydown, b.input = wrap(b.onClick), wrap(b.onSubmit), wrap(b.onKey), wrap(b.onInput)
	doc.Call("addEventListener", "click", b.click)
	doc.Call("addEventListener", "submit", b.submit)
	doc.Call("addEventListener", "keydown", b.keydown, true)
	doc.Call("addEventListener", "input", b.input)
	return func() {
		b.disposed = true
		b.closeDialog(false)
		doc.Call("removeEventListener", "click", b.click)
		doc.Call("removeEventListener", "submit", b.submit)
		doc.Call("removeEventListener", "keydown", b.keydown, true)
		doc.Call("removeEventListener", "input", b.input)
		b.click.Release()
		b.submit.Release()
		b.keydown.Release()
		b.input.Release()
		if decisionsActive == b {
			decisionsActive = nil
		}
	}
}

// decisionsProject gives the page's model /decide and the Decisions row. They
// are offered only once the server has answered a read of this conversation's
// decisions, so there is never a command or a button that cannot work. The read
// starts here when it has not happened for the open conversation.
func decisionsProject(model chatui.Model, _ journeyclient.Config) chatui.Model {
	b := decisionsActive
	if b == nil || b.disposed {
		return model
	}
	b.mu.Lock()
	state := chatui.DecisionsState{Record: b.openRecord, OpenList: b.openList}
	if b.loadedOK && b.loadedConv == model.SelectedID {
		state.Enabled = true
		if b.canRecord {
			// Marking a message records a decision, so it is offered only to
			// someone the server said may record one here.
			state.Mark = b.openMark
		}
		state.Count = decisionsInForce(b.items)
	}
	want := model.SelectedID != "" && b.loadedConv != model.SelectedID && b.loading != model.SelectedID &&
		(b.failedConv != model.SelectedID || time.Since(b.failedAt) > 30*time.Second)
	if want {
		b.loading = model.SelectedID
	}
	stale := b.open && b.conversationID != model.SelectedID
	b.mu.Unlock()
	model.Decisions = state
	if want {
		go b.load(model.SelectedID)
	}
	if stale {
		ui.PostAsync(func() { b.closeDialog(false) })
	}
	return model
}

func (b *decisionsBrowser) context() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 15*time.Second)
}

func (b *decisionsBrowser) host(conversation string) string {
	return channelTodoHost(conversation, b.cfg.Tenant)
}

// load reads the conversation's decisions and, when its dialog is open, shows
// them. A failed read of a closed conversation leaves the commands out.
func (b *decisionsBrowser) load(conversation string) {
	ctx, cancel := b.context()
	defer cancel()
	reply, err := decisionsCall(ctx, http.DefaultClient, b.cfg, "list", decisionsCommand{HostTenantID: b.host(conversation), ConversationID: conversation})
	ui.PostAsync(func() {
		model := chatBrowser.snapshot()
		you, unknown := chatui.DecisionsOwnerLabels(b.cfg.Locale)
		b.mu.Lock()
		if b.loading == conversation {
			b.loading = ""
		}
		if err != nil {
			b.failedConv, b.failedAt = conversation, time.Now()
			if b.loadedConv == conversation {
				b.loadedOK = false
			}
			redraw := b.open && b.conversationID == conversation
			if redraw {
				b.view.State, b.view.Error = "error", decisionsErrorCode(err)
				if len(b.view.Items) > 0 {
					b.view.State = "ready"
				}
			}
			b.mu.Unlock()
			if redraw {
				b.render(false)
			}
			return
		}
		b.items, b.canRecord, b.loadedConv, b.loadedOK, b.failedConv = reply.Decisions, reply.CanRecord, conversation, true, ""
		redraw := b.open && b.conversationID == conversation
		if redraw {
			b.view.Items, b.view.CanRecord, b.view.State = decisionsItems(model, reply.Decisions, you, unknown), reply.CanRecord, "ready"
		}
		b.mu.Unlock()
		if redraw {
			b.render(false)
		}
		refreshChatRoute()
	})
}

func (b *decisionsBrowser) channelName(model chatui.Model, conversation string) string {
	for _, c := range model.Conversations {
		if c.ID == conversation {
			return c.Name
		}
	}
	return ""
}

// openRecord opens the dialog with the decision's words typed in. It is what
// /decide runs; nothing is recorded until Record decision is pressed.
func (b *decisionsBrowser) openRecord(text, parent string) {
	b.openDialog(text, parent, "#chat-composer", "")
}

// openList opens the dialog on the conversation's decisions.
func (b *decisionsBrowser) openList() { b.openDialog("", "", `[data-decisions-open]`, "") }

// openMark opens the dialog for a message the person chose from its menu. The
// message's words are the decision and the record points at that message.
func (b *decisionsBrowser) openMark(postID, text string) {
	focus := "#chat-composer"
	if postID != "" && strings.Trim(postID, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_") == "" {
		focus = `[data-message-id="` + postID + `"]`
	}
	b.openDialog(text, "", focus, postID)
}

func (b *decisionsBrowser) openDialog(text, parent, returnFocus, source string) {
	model := chatBrowser.snapshot()
	if model.SelectedID == "" || b.disposed {
		return
	}
	you, unknown := chatui.DecisionsOwnerLabels(b.cfg.Locale)
	b.mu.Lock()
	b.generation++
	b.open, b.conversationID, b.parent, b.key, b.returnFocus, b.sourcePost = true, model.SelectedID, parent, decisionsNewKey(), returnFocus, source
	b.view = chatui.DecisionsDialogView{Locale: b.cfg.Locale, Channel: b.channelName(model, model.SelectedID), State: "loading", Text: text, FromMessage: source != ""}
	if b.loadedOK && b.loadedConv == model.SelectedID {
		b.view.State, b.view.CanRecord, b.view.Items = "ready", b.canRecord, decisionsItems(model, b.items, you, unknown)
	}
	b.mu.Unlock()
	b.render(true)
	go b.load(model.SelectedID)
}

func (b *decisionsBrowser) mount() js.Value {
	doc := js.Global().Get("document")
	mount := doc.Call("querySelector", "[data-decisions-mount]")
	if !mount.Truthy() {
		mount = doc.Call("createElement", "div")
		mount.Call("setAttribute", "data-decisions-mount", "true")
		doc.Get("body").Call("appendChild", mount)
	}
	return mount
}

// readForm copies what the person has typed into the view, so a redraw (the
// list arriving, a refusal) never loses it.
func (b *decisionsBrowser) readForm(mount js.Value) {
	if field := mount.Call("querySelector", "#decisions-field-text"); field.Truthy() {
		b.view.Text = field.Get("value").String()
	}
	if field := mount.Call("querySelector", "#decisions-field-why"); field.Truthy() {
		b.view.Rationale = field.Get("value").String()
	}
}

// render draws the dialog. firstFocus moves focus into it (the decision field
// where the person may record, else the heading); later draws keep the field
// the person is in.
func (b *decisionsBrowser) render(firstFocus bool) {
	doc := js.Global().Get("document")
	mount := b.mount()
	b.mu.Lock()
	if !b.open {
		b.mu.Unlock()
		return
	}
	b.readForm(mount)
	view := b.view
	b.mu.Unlock()
	markup, err := ui.RenderToString(chatui.RenderDecisionsDialog(view))
	if err != nil {
		return
	}
	keep, scroll := "", 0.0
	if active := doc.Get("activeElement"); active.Truthy() && mount.Call("contains", active).Bool() {
		keep = active.Get("id").String()
	}
	if dialog := mount.Call("querySelector", ".decisions-dialog"); dialog.Truthy() {
		scroll = dialog.Get("scrollTop").Float()
	}
	mount.Set("innerHTML", markup)
	if dialog := mount.Call("querySelector", ".decisions-dialog"); dialog.Truthy() && scroll > 0 {
		dialog.Set("scrollTop", scroll)
	}
	target := js.Null()
	switch {
	case keep != "":
		target = doc.Call("getElementById", keep)
	case view.Error != "" && view.CanRecord:
		target = mount.Call("querySelector", "#decisions-field-text")
	}
	if !target.Truthy() && (firstFocus || keep == "") {
		for _, selector := range []string{"#decisions-field-text", "[data-decisions-close]", ".decisions-dialog"} {
			if candidate := mount.Call("querySelector", selector); candidate.Truthy() {
				target = candidate
				break
			}
		}
	}
	if target.Truthy() && target.Get("focus").Type() == js.TypeFunction {
		if target.Get("classList").Call("contains", "decisions-dialog").Bool() {
			target.Call("setAttribute", "tabindex", "-1")
		}
		target.Call("focus")
	}
}

func (b *decisionsBrowser) closeDialog(restoreFocus bool) {
	b.mu.Lock()
	wasOpen, focus := b.open, b.returnFocus
	b.open = false
	b.generation++
	b.mu.Unlock()
	doc := js.Global().Get("document")
	if mount := doc.Call("querySelector", "[data-decisions-mount]"); mount.Truthy() {
		mount.Set("innerHTML", "")
	}
	if !wasOpen || !restoreFocus {
		return
	}
	for _, selector := range []string{focus, "#chat-composer"} {
		if selector == "" {
			continue
		}
		if target := doc.Call("querySelector", selector); target.Truthy() {
			target.Call("focus")
			return
		}
	}
}

func (b *decisionsBrowser) busy() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.view.Busy
}

func (b *decisionsBrowser) onClick(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	// The Decisions row of Conversation details opens the dialog.
	if opener := target.Call("closest", "[data-decisions-open]"); opener.Truthy() && !b.open {
		event.Call("preventDefault")
		b.openList()
		return
	}
	if !b.open {
		return
	}
	if target.Call("matches", "[data-decisions-overlay]").Bool() || target.Call("closest", "[data-decisions-close]").Truthy() {
		event.Call("preventDefault")
		if !b.busy() {
			b.closeDialog(true)
		}
		return
	}
	if goto_ := target.Call("closest", "[data-decisions-goto]"); goto_.Truthy() {
		event.Call("preventDefault")
		post := goto_.Call("getAttribute", "data-decisions-goto").String()
		var sequence uint64
		for _, r := range goto_.Call("getAttribute", "data-decisions-seq").String() {
			if r < '0' || r > '9' {
				sequence = 0
				break
			}
			sequence = sequence*10 + uint64(r-'0')
		}
		b.mu.Lock()
		conversation := b.conversationID
		b.mu.Unlock()
		b.closeDialog(false)
		if open := chatBrowser.snapshot().Callbacks.OpenSearchMessage; open != nil && post != "" {
			open(conversation, post, sequence)
		}
		return
	}
	if replace := target.Call("closest", "[data-decisions-replace]"); replace.Truthy() {
		event.Call("preventDefault")
		id := replace.Call("getAttribute", "data-decisions-replace").String()
		b.mu.Lock()
		b.readForm(b.mount())
		b.view.ReplacingID, b.view.Error, b.key = id, "", decisionsNewKey()
		if strings.TrimSpace(b.view.Text) == "" {
			for _, item := range b.view.Items {
				if item.ID == id {
					b.view.Text = item.Text
				}
			}
		}
		b.mu.Unlock()
		b.render(false)
		if field := b.mount().Call("querySelector", "#decisions-field-text"); field.Truthy() {
			field.Call("focus")
		}
		return
	}
	if target.Call("closest", "[data-decisions-keep]").Truthy() {
		event.Call("preventDefault")
		b.mu.Lock()
		b.view.ReplacingID, b.view.Error, b.key = "", "", decisionsNewKey()
		b.mu.Unlock()
		b.render(false)
		if field := b.mount().Call("querySelector", "#decisions-field-text"); field.Truthy() {
			field.Call("focus")
		}
	}
}

func (b *decisionsBrowser) onKey(event js.Value) {
	if !b.open {
		return
	}
	switch event.Get("key").String() {
	case "Escape":
		event.Call("preventDefault")
		event.Call("stopImmediatePropagation")
		if !b.busy() {
			b.closeDialog(true)
		}
	case "Tab":
		dialog := b.mount().Call("querySelector", ".decisions-dialog")
		if !dialog.Truthy() {
			return
		}
		controls := dialog.Call("querySelectorAll", "button:not([disabled]),textarea:not([disabled]),input:not([disabled]),summary,[tabindex]:not([tabindex='-1'])")
		if controls.Length() == 0 {
			event.Call("preventDefault")
			dialog.Call("focus")
			return
		}
		first, last := controls.Index(0), controls.Index(controls.Length()-1)
		active := js.Global().Get("document").Get("activeElement")
		backward := event.Get("shiftKey").Bool()
		if backward && (active.Equal(first) || active.Equal(dialog)) {
			event.Call("preventDefault")
			last.Call("focus")
		} else if !backward && active.Equal(last) {
			event.Call("preventDefault")
			first.Call("focus")
		}
	}
}

// onInput starts a new attempt when the person changes what they wrote, so a
// retry of the same request recovers its decision and an edited request is a
// new one.
func (b *decisionsBrowser) onInput(event js.Value) {
	target := event.Get("target")
	if !b.open || !target.Truthy() || target.Get("closest").Type() != js.TypeFunction || !target.Call("closest", ".decisions-form").Truthy() {
		return
	}
	b.mu.Lock()
	b.key = decisionsNewKey()
	b.mu.Unlock()
}

func (b *decisionsBrowser) onSubmit(event js.Value) {
	form := event.Get("target").Call("closest", "form[data-decisions-form]")
	if !form.Truthy() || !b.open {
		return
	}
	event.Call("preventDefault")
	mount := b.mount()
	b.mu.Lock()
	if b.view.Busy {
		b.mu.Unlock()
		return
	}
	b.readForm(mount)
	view, key, conversation, parent, source := b.view, b.key, b.conversationID, b.parent, b.sourcePost
	if strings.TrimSpace(view.Text) == "" {
		b.view.Error = "text"
	} else {
		b.view.Error, b.view.Busy, b.view.Notice = "", true, ""
	}
	invalid := !b.view.Busy
	generation := b.generation
	b.mu.Unlock()
	b.render(false)
	if invalid {
		if field := mount.Call("querySelector", "#decisions-field-text"); field.Truthy() {
			field.Call("focus")
		}
		return
	}
	command := decisionsCommand{HostTenantID: b.host(conversation), ConversationID: conversation, Text: strings.TrimSpace(view.Text), Rationale: strings.TrimSpace(view.Rationale),
		SupersedesID: view.ReplacingID, SourcePostID: source, ParentID: parent, IdempotencyKey: key, Label: chatui.DecisionsPostLabel(b.cfg.Locale)}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := decisionsCall(ctx, http.DefaultClient, b.cfg, "record", command)
		ui.PostAsync(func() {
			b.mu.Lock()
			if !b.open || b.generation != generation {
				b.mu.Unlock()
				return
			}
			b.view.Busy = false
			if err != nil {
				b.view.Error = decisionsErrorCode(err)
				b.mu.Unlock()
				b.render(false)
				if decisionsErrorCode(err) == "conflict" || decisionsErrorCode(err) == "unavailable" {
					// The answer may have been lost after the decision was
					// recorded, or it was replaced meanwhile: read the list again.
					go b.load(conversation)
				}
				return
			}
			done := chatui.DecisionsDoneText(b.cfg.Locale)
			b.view.Text, b.view.Rationale, b.view.ReplacingID, b.view.Error, b.view.Notice = "", "", "", "", done
			b.view.FromMessage, b.sourcePost = false, ""
			b.key = decisionsNewKey()
			b.mu.Unlock()
			b.render(false)
			go b.load(conversation)
			// The line the decision posted arrives with the conversation's own
			// stream; reading forward as well shows it at once between connections.
			go catchUpChatConversation(context.Background(), conversation, chatBrowser.currentGeneration(), b.cfg)
		})
	}()
}
