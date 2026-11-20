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

// Issue 3794: the browser half of handing off one linked work item. It owns the
// offer form (drawn over the page, with its own focus handling), keeps the
// viewer's offers for the open conversation for the cards under each task, and
// sends accept, decline and withdraw. The server decides everything.

const handoffRefresh = 30 * time.Second

type handoffBrowser struct {
	mu       sync.Mutex
	cfg      journeyclient.Config
	disposed bool
	// enabled is true once the server has answered a read of the offers.
	enabled bool
	offers  map[string][]chatui.WorkHandoffView
	// loadedConv/loadedAt say which conversation the offers are for and how
	// fresh; failedAt rate-limits a failed read.
	loadedConv string
	loadedAt   time.Time
	loading    string
	failedConv string
	failedAt   time.Time
	busy       map[string]bool
	// The form.
	open          bool
	view          chatui.WorkHandoffDialogView
	conversation  string
	key           string
	generation    uint64
	click, submit js.Func
	keydown       js.Func
	input         js.Func
}

var handoffActive *handoffBrowser

func installWorkHandoff(cfg journeyclient.Config) func() {
	cfg = personaChatHTTPConfig(cfg)
	b := &handoffBrowser{cfg: cfg, busy: map[string]bool{}}
	handoffActive = b
	doc := js.Global().Get("document")
	wrap := func(call func(js.Value)) js.Func {
		return js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 && !b.disposed {
				call(args[0])
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
		if handoffActive == b {
			handoffActive = nil
		}
	}
}

// handoffProject adds the hand-off actions and the viewer's offers to the model
// the page draws. It runs as the page is built; reading the open conversation's
// offers starts here when they are missing or older than handoffRefresh.
func handoffProject(model chatui.Model) chatui.Model {
	b := handoffActive
	if b == nil || b.disposed {
		return model
	}
	b.mu.Lock()
	state := chatui.WorkHandoffState{Offer: b.openFor, Answer: b.answer, Busy: map[string]bool{}}
	for id := range b.busy {
		state.Busy[id] = true
	}
	hosted := false
	for _, conversation := range model.Conversations {
		if conversation.ID == model.SelectedID && conversation.HostTenantID != "" && conversation.HostTenantID != model.CurrentTenantID {
			hosted = true
		}
	}
	// Offers are only offered where the work chips are: the server must have
	// answered the linked-work read, and the conversation is this company's.
	workReady := model.MessageWork.Enabled && !hosted && model.SelectedID != ""
	state.Enabled = workReady && b.enabled
	if b.loadedConv == model.SelectedID {
		state.ByTask = b.offers
	}
	stale := b.loadedConv != model.SelectedID || time.Since(b.loadedAt) > handoffRefresh
	want := workReady && stale && b.loading == "" && (b.failedConv != model.SelectedID || time.Since(b.failedAt) > handoffRefresh)
	if want {
		b.loading = model.SelectedID
	}
	b.mu.Unlock()
	model.WorkHandoff = state
	if want {
		go b.load(model.SelectedID)
	}
	return model
}

func (b *handoffBrowser) context() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 15*time.Second)
}

func (b *handoffBrowser) load(conversation string) {
	ctx, cancel := b.context()
	defer cancel()
	offers, err := handoffList(ctx, http.DefaultClient, b.cfg, conversation)
	ui.PostAsync(func() {
		b.mu.Lock()
		if b.loading == conversation {
			b.loading = ""
		}
		if err != nil {
			b.failedConv, b.failedAt = conversation, time.Now()
			b.mu.Unlock()
			return
		}
		changed := b.loadedConv != conversation || !handoffSame(b.offers, handoffByTask(offers))
		b.offers, b.loadedConv, b.loadedAt, b.failedConv, b.enabled = handoffByTask(offers), conversation, time.Now(), "", true
		b.mu.Unlock()
		if changed {
			refreshChatRoute()
		}
	})
}

// handoffSame reports whether a fresh read shows nothing new, so a quiet refresh
// does not redraw the page under the person's hands.
func handoffSame(a, b map[string][]chatui.WorkHandoffView) bool {
	if len(a) != len(b) {
		return false
	}
	for task, list := range a {
		other := b[task]
		if len(list) != len(other) {
			return false
		}
		for i := range list {
			if list[i] != other[i] {
				return false
			}
		}
	}
	return true
}

func (b *handoffBrowser) mount() js.Value {
	doc := js.Global().Get("document")
	mount := doc.Call("querySelector", "[data-chathandoff-mount]")
	if !mount.Truthy() {
		mount = doc.Call("createElement", "div")
		mount.Call("setAttribute", "data-chathandoff-mount", "true")
		doc.Get("body").Call("appendChild", mount)
	}
	return mount
}

// openFor opens the offer form for one linked task of the open conversation.
func (b *handoffBrowser) openFor(postID, projectID, taskID, title string) {
	model := chatBrowser.snapshot()
	if model.SelectedID == "" || b.disposed {
		return
	}
	b.mu.Lock()
	b.generation++
	b.open, b.conversation, b.key = true, model.SelectedID, handoffNewKey()
	b.view = chatui.WorkHandoffDialogView{Locale: b.cfg.Locale, PostID: postID, ProjectID: projectID, TaskID: taskID, Title: title, Mode: "FOLLOW_UP", People: chatui.WorkHandoffCandidates(model)}
	b.mu.Unlock()
	b.render(true)
}

// readForm copies what the person has typed into the view, so a redraw (a
// refusal) never loses it.
func (b *handoffBrowser) readForm(mount js.Value) {
	read := func(selector string) (string, bool) {
		field := mount.Call("querySelector", selector)
		if !field.Truthy() {
			return "", false
		}
		return field.Get("value").String(), true
	}
	if v, ok := read("#chathandoff-field-to"); ok {
		b.view.RecipientID = v
	}
	if v, ok := read("#chathandoff-field-action"); ok {
		b.view.NextAction = v
	}
	if v, ok := read("#chathandoff-field-note"); ok {
		b.view.Note = v
	}
	if v, ok := read("input[name=mode]:checked"); ok {
		b.view.Mode = v
	}
}

// render draws the form. firstFocus moves focus into it; later draws keep the
// field the person is in.
func (b *handoffBrowser) render(firstFocus bool) {
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
	markup, err := ui.RenderToString(chatui.RenderWorkHandoffDialog(view))
	if err != nil {
		return
	}
	keep := ""
	if active := doc.Get("activeElement"); active.Truthy() && mount.Call("contains", active).Bool() {
		keep = active.Get("id").String()
	}
	mount.Set("innerHTML", markup)
	target := js.Null()
	switch {
	case keep != "":
		target = doc.Call("getElementById", keep)
	case view.Error != "":
		target = mount.Call("querySelector", "#chathandoff-field-to")
	}
	if !target.Truthy() && (firstFocus || keep == "") {
		for _, selector := range []string{"#chathandoff-field-to", "[data-chathandoff-close]", ".chathandoff-dialog"} {
			if candidate := mount.Call("querySelector", selector); candidate.Truthy() {
				target = candidate
				break
			}
		}
	}
	if target.Truthy() && target.Get("focus").Type() == js.TypeFunction {
		if target.Get("tabIndex").Int() < 0 || target.Get("classList").Call("contains", "chathandoff-dialog").Bool() {
			target.Call("setAttribute", "tabindex", "-1")
		}
		target.Call("focus")
	}
}

func (b *handoffBrowser) closeDialog(restoreFocus bool) {
	b.mu.Lock()
	task := b.view.TaskID
	wasOpen := b.open
	b.open = false
	b.generation++
	b.mu.Unlock()
	doc := js.Global().Get("document")
	if mount := doc.Call("querySelector", "[data-chathandoff-mount]"); mount.Truthy() {
		mount.Set("innerHTML", "")
	}
	if !wasOpen || !restoreFocus {
		return
	}
	// Focus goes back to the Hand off button the form was opened from.
	for _, selector := range []string{`.chathandoff-open[data-id="` + cssEscape(task) + `"]`, "#chat-composer"} {
		if target := doc.Call("querySelector", selector); target.Truthy() {
			target.Call("focus")
			return
		}
	}
}

func (b *handoffBrowser) isBusy() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.view.Busy
}

func (b *handoffBrowser) onClick(event js.Value) {
	target := event.Get("target")
	if !b.open || !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	closer := target.Call("closest", "[data-chathandoff-close]")
	backdrop := target.Call("matches", "[data-chathandoff-overlay]").Bool()
	if !closer.Truthy() && !backdrop {
		return
	}
	event.Call("preventDefault")
	if !b.isBusy() {
		b.closeDialog(true)
	}
}

func (b *handoffBrowser) onKey(event js.Value) {
	if !b.open {
		return
	}
	switch event.Get("key").String() {
	case "Escape":
		event.Call("preventDefault")
		event.Call("stopImmediatePropagation")
		if !b.isBusy() {
			b.closeDialog(true)
		}
	case "Tab":
		dialog := b.mount().Call("querySelector", ".chathandoff-dialog")
		if !dialog.Truthy() {
			return
		}
		controls := dialog.Call("querySelectorAll", "button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled])")
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

// onInput starts a new attempt when the person changes what they asked for, so
// a retry of the same reviewed offer recovers it and an edited offer is new.
func (b *handoffBrowser) onInput(event js.Value) {
	target := event.Get("target")
	if !b.open || !target.Truthy() || target.Get("closest").Type() != js.TypeFunction || !target.Call("closest", ".chathandoff-form").Truthy() {
		return
	}
	b.mu.Lock()
	b.key = handoffNewKey()
	b.mu.Unlock()
}

func (b *handoffBrowser) onSubmit(event js.Value) {
	form := event.Get("target").Call("closest", "form[data-chathandoff-form]")
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
	view, key, conversation := b.view, b.key, b.conversation
	switch {
	case view.RecipientID == "":
		b.view.Error = "person"
	case strings.TrimSpace(view.NextAction) == "":
		b.view.Error = "action"
	default:
		b.view.Error, b.view.Busy = "", true
	}
	invalid := !b.view.Busy
	generation := b.generation
	b.mu.Unlock()
	b.render(false)
	if invalid {
		field := "#chathandoff-field-to"
		if view.RecipientID != "" {
			field = "#chathandoff-field-action"
		}
		if target := mount.Call("querySelector", field); target.Truthy() {
			target.Call("focus")
		}
		return
	}
	command := handoffOffer{ConversationID: conversation, ProjectID: view.ProjectID, TaskID: view.TaskID, Mode: view.Mode, RecipientID: view.RecipientID,
		NextAction: strings.TrimSpace(view.NextAction), Note: strings.TrimSpace(view.Note), IdempotencyKey: key}
	go func() {
		ctx, cancel := b.context()
		defer cancel()
		offer, err := handoffSend(ctx, http.DefaultClient, b.cfg, command)
		ui.PostAsync(func() {
			b.mu.Lock()
			if !b.open || b.generation != generation {
				b.mu.Unlock()
				return
			}
			b.view.Busy = false
			if err != nil {
				b.view.Error = chatworkErrorCode(err)
				b.mu.Unlock()
				b.render(false)
				if chatworkErrorCode(err) == "unavailable" {
					// The answer may have been lost after the offer was kept:
					// read what exists so the same offer is recovered, not repeated.
					b.reload(conversation)
				}
				return
			}
			if b.loadedConv == conversation {
				b.offers = handoffMerge(b.offers, offer)
			}
			b.mu.Unlock()
			b.closeDialog(true)
			b.announce(chatui.WorkHandoffDoneText(b.cfg.Locale))
			b.reload(conversation)
			refreshChatRoute()
		})
	}()
}

// reload reads the conversation's offers again now.
func (b *handoffBrowser) reload(conversation string) {
	b.mu.Lock()
	b.loadedAt = time.Time{}
	if b.loading != "" {
		b.mu.Unlock()
		return
	}
	b.loading = conversation
	b.mu.Unlock()
	go b.load(conversation)
}

// answer sends accept, decline or withdraw for one offer, then reads the state
// back so both people see what really happened.
func (b *handoffBrowser) answer(id, verb string) {
	model := chatBrowser.snapshot()
	conversation := model.SelectedID
	b.mu.Lock()
	if b.busy[id] || conversation == "" || b.disposed {
		b.mu.Unlock()
		return
	}
	b.busy[id] = true
	b.mu.Unlock()
	refreshChatRoute()
	go func() {
		ctx, cancel := b.context()
		defer cancel()
		offer, err := handoffAnswer(ctx, http.DefaultClient, b.cfg, id, verb)
		ui.PostAsync(func() {
			b.mu.Lock()
			delete(b.busy, id)
			if err == nil && b.loadedConv == conversation {
				b.offers = handoffMerge(b.offers, offer)
			}
			b.mu.Unlock()
			if err != nil {
				b.announce(chatui.WorkHandoffErrorText(b.cfg.Locale, chatworkErrorCode(err)))
			} else {
				b.announce(chatui.WorkHandoffDoneText(b.cfg.Locale))
			}
			// A refusal or an uncertain answer is settled by reading what is true now.
			b.reload(conversation)
			refreshChatRoute()
		})
	}()
}

// announce tells a screen reader what happened.
func (b *handoffBrowser) announce(text string) {
	doc := js.Global().Get("document")
	live := doc.Call("querySelector", "[data-chathandoff-live]")
	if !live.Truthy() {
		live = doc.Call("createElement", "div")
		live.Call("setAttribute", "data-chathandoff-live", "true")
		live.Call("setAttribute", "role", "status")
		live.Call("setAttribute", "aria-live", "polite")
		live.Call("setAttribute", "style", "position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap")
		doc.Get("body").Call("appendChild", live)
	}
	live.Set("textContent", text)
}
