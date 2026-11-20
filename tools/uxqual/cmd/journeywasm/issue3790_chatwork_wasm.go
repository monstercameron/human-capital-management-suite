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

// Issue 3790: the browser half of "turn a message into a task". It owns the
// Create task form (drawn over the page, with its own focus handling), asks the
// server for the projects a task can go to, sends the reviewed values, and keeps
// what each message of the open conversation became for the chips under it.

type chatworkBrowser struct {
	mu       sync.Mutex
	cfg      journeyclient.Config
	disposed bool
	// enabled is true once the server has answered the projects read.
	enabled, probing bool
	// links are the viewer's linked work for linkedConv; failedAt rate-limits a
	// failed read of the next conversation.
	links      map[string][]chatui.MessageWorkLink
	linkedConv string
	loading    string
	failedConv string
	failedAt   time.Time
	// The form.
	open           bool
	view           chatui.MessageWorkDialogView
	conversationID string
	key            string
	generation     uint64
	click, submit  js.Func
	keydown, input js.Func
}

var chatworkActive *chatworkBrowser

func installChatWork(cfg journeyclient.Config) func() {
	cfg = personaChatHTTPConfig(cfg)
	b := &chatworkBrowser{cfg: cfg}
	chatworkActive = b
	doc := js.Global().Get("document")
	b.click = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && !b.disposed {
			b.onClick(args[0])
		}
		return nil
	})
	b.submit = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && !b.disposed {
			b.onSubmit(args[0])
		}
		return nil
	})
	b.keydown = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && !b.disposed {
			b.onKey(args[0])
		}
		return nil
	})
	b.input = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && !b.disposed {
			b.onInput(args[0])
		}
		return nil
	})
	doc.Call("addEventListener", "click", b.click)
	doc.Call("addEventListener", "submit", b.submit)
	doc.Call("addEventListener", "keydown", b.keydown, true)
	doc.Call("addEventListener", "input", b.input)
	go b.probe()
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
		if chatworkActive == b {
			chatworkActive = nil
		}
	}
}

// chatworkProject adds the Create task action and the viewer's linked work to
// the model the page draws. It is called as the page is built; reading the
// open conversation's links starts here when they are not loaded yet.
func chatworkProject(model chatui.Model, _ journeyclient.Config) chatui.Model {
	b := chatworkActive
	if b == nil || b.disposed {
		return model
	}
	b.mu.Lock()
	state := chatui.MessageWorkState{Enabled: b.enabled, Open: b.openFor}
	// Hosted-elsewhere conversations are not served by this tenant's routes.
	for _, conversation := range model.Conversations {
		if conversation.ID == model.SelectedID && conversation.HostTenantID != "" && conversation.HostTenantID != model.CurrentTenantID {
			state.Enabled = false
		}
	}
	if b.linkedConv == model.SelectedID && b.enabled {
		state.Links = b.links
	}
	want := b.enabled && model.SelectedID != "" && b.linkedConv != model.SelectedID && b.loading != model.SelectedID &&
		(b.failedConv != model.SelectedID || time.Since(b.failedAt) > 30*time.Second)
	if want {
		b.loading = model.SelectedID
	}
	b.mu.Unlock()
	model.MessageWork = state
	if want {
		go b.loadLinks(model.SelectedID)
	}
	return model
}

func (b *chatworkBrowser) context() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 15*time.Second)
}

// probe asks whether the server can make tasks for this person. Until it says
// so the menu has no Create task row, so there is never a row that cannot work.
func (b *chatworkBrowser) probe() {
	b.mu.Lock()
	if b.probing || b.enabled {
		b.mu.Unlock()
		return
	}
	b.probing = true
	b.mu.Unlock()
	ctx, cancel := b.context()
	defer cancel()
	_, err := chatworkProjects(ctx, http.DefaultClient, b.cfg)
	ui.PostAsync(func() {
		b.mu.Lock()
		b.probing = false
		b.enabled = err == nil
		b.mu.Unlock()
		if err == nil {
			refreshChatRoute()
		}
	})
}

func (b *chatworkBrowser) loadLinks(conversation string) {
	ctx, cancel := b.context()
	defer cancel()
	links, err := chatworkLinked(ctx, http.DefaultClient, b.cfg, conversation)
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
		b.links, b.linkedConv, b.failedConv = chatworkByPost(links), conversation, ""
		existing := b.existingFor(b.view.PostID)
		redraw := b.open && b.conversationID == conversation
		if redraw {
			b.view.Existing = existing
		}
		b.mu.Unlock()
		if redraw {
			b.render(false)
		}
		refreshChatRoute()
	})
}

func (b *chatworkBrowser) existingFor(post string) []chatui.MessageWorkLink {
	if post == "" || b.linkedConv != b.conversationID {
		return nil
	}
	return append([]chatui.MessageWorkLink(nil), b.links[post]...)
}

func chatworkFindMessage(model chatui.Model, id string) (chatui.Message, bool) {
	for _, list := range [][]chatui.Message{model.Messages, model.ThreadMessages} {
		for _, msg := range list {
			if msg.ID == id {
				return msg, true
			}
		}
	}
	if model.ThreadParent != nil && model.ThreadParent.ID == id {
		return *model.ThreadParent, true
	}
	return chatui.Message{}, false
}

// openFor opens the form for one message of the open conversation.
func (b *chatworkBrowser) openFor(postID string) {
	model := chatBrowser.snapshot()
	msg, found := chatworkFindMessage(model, postID)
	if !found || model.SelectedID == "" || b.disposed {
		return
	}
	me, author := chatui.MessageWorkOwnerLabels(b.cfg.Locale)
	b.mu.Lock()
	b.generation++
	generation := b.generation
	b.open, b.conversationID, b.key = true, model.SelectedID, chatworkNewKey()
	b.view = chatui.MessageWorkDialogView{Locale: b.cfg.Locale, PostID: postID, Title: chatworkTitle(msg.Body), OwnerID: model.CurrentUser,
		Owners: chatworkOwners(model, msg, me, author), State: "loading"}
	b.view.Existing = b.existingFor(postID)
	b.mu.Unlock()
	b.render(true)
	go func() {
		ctx, cancel := b.context()
		defer cancel()
		projects, err := chatworkProjects(ctx, http.DefaultClient, b.cfg)
		ui.PostAsync(func() {
			b.mu.Lock()
			if !b.open || b.generation != generation {
				b.mu.Unlock()
				return
			}
			b.view.Projects, b.view.State, b.view.Error = projects, "ready", ""
			if err != nil {
				b.view.State, b.view.Error = "error", chatworkErrorCode(err)
			} else if len(projects) == 1 {
				b.view.ProjectID = projects[0].ID
			}
			b.mu.Unlock()
			b.render(true)
		})
	}()
}

func (b *chatworkBrowser) mount() js.Value {
	doc := js.Global().Get("document")
	mount := doc.Call("querySelector", "[data-chatwork-mount]")
	if !mount.Truthy() {
		mount = doc.Call("createElement", "div")
		mount.Call("setAttribute", "data-chatwork-mount", "true")
		doc.Get("body").Call("appendChild", mount)
	}
	return mount
}

// readForm copies what the person has typed into the view, so a redraw (the
// projects arriving, a refusal) never loses it.
func (b *chatworkBrowser) readForm(mount js.Value) {
	read := func(id string) (string, bool) {
		field := mount.Call("querySelector", "#"+id)
		if !field.Truthy() {
			return "", false
		}
		return field.Get("value").String(), true
	}
	if v, ok := read("chatwork-field-title"); ok {
		b.view.Title = v
	}
	if v, ok := read("chatwork-field-project"); ok {
		b.view.ProjectID = v
	}
	if v, ok := read("chatwork-field-owner"); ok {
		b.view.OwnerID = v
	}
	if v, ok := read("chatwork-field-due"); ok {
		b.view.DueDay = v
	}
}

// render draws the form. firstFocus moves focus into it (the title, or the
// heading while it loads); later draws keep the field the person is in.
func (b *chatworkBrowser) render(firstFocus bool) {
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
	markup, err := ui.RenderToString(chatui.RenderMessageWorkDialog(view))
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
	case view.Error != "" && view.State == "ready":
		target = mount.Call("querySelector", "#chatwork-field-title")
	}
	if !target.Truthy() && (firstFocus || keep == "") {
		for _, selector := range []string{"#chatwork-field-title", "[data-chatwork-close]", ".chatwork-dialog"} {
			if candidate := mount.Call("querySelector", selector); candidate.Truthy() {
				target = candidate
				break
			}
		}
	}
	if target.Truthy() && target.Get("focus").Type() == js.TypeFunction {
		if target.Get("tabIndex").Int() < 0 || target.Get("classList").Call("contains", "chatwork-dialog").Bool() {
			target.Call("setAttribute", "tabindex", "-1")
		}
		target.Call("focus")
	}
}

func (b *chatworkBrowser) closeDialog(restoreFocus bool) {
	b.mu.Lock()
	post := b.view.PostID
	wasOpen := b.open
	b.open = false
	b.generation++
	b.mu.Unlock()
	doc := js.Global().Get("document")
	if mount := doc.Call("querySelector", "[data-chatwork-mount]"); mount.Truthy() {
		mount.Set("innerHTML", "")
	}
	if !wasOpen || !restoreFocus {
		return
	}
	// Focus goes back to the message the form was opened from.
	for _, selector := range []string{
		`.message-action[data-action="menu"][data-id="` + cssEscape(post) + `"]`,
		`[data-message-id="` + cssEscape(post) + `"]`,
		"#chat-composer",
	} {
		if target := doc.Call("querySelector", selector); target.Truthy() {
			if selector[0] == '[' {
				target.Call("setAttribute", "tabindex", "-1")
			}
			target.Call("focus")
			return
		}
	}
}

func cssEscape(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

func (b *chatworkBrowser) busy() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.view.Busy
}

func (b *chatworkBrowser) onClick(event js.Value) {
	target := event.Get("target")
	if !b.open || !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	closer := target.Call("closest", "[data-chatwork-close]")
	backdrop := target.Call("matches", "[data-chatwork-overlay]").Bool()
	if !closer.Truthy() && !backdrop {
		return
	}
	event.Call("preventDefault")
	if !b.busy() {
		b.closeDialog(true)
	}
}

func (b *chatworkBrowser) onKey(event js.Value) {
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
		dialog := b.mount().Call("querySelector", ".chatwork-dialog")
		if !dialog.Truthy() {
			return
		}
		controls := dialog.Call("querySelectorAll", "button:not([disabled]),input:not([disabled]),select:not([disabled])")
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
// a retry of the same reviewed request recovers its task and an edited request
// is a new one.
func (b *chatworkBrowser) onInput(event js.Value) {
	target := event.Get("target")
	if !b.open || !target.Truthy() || target.Get("closest").Type() != js.TypeFunction || !target.Call("closest", ".chatwork-form").Truthy() {
		return
	}
	b.mu.Lock()
	b.key = chatworkNewKey()
	b.mu.Unlock()
}

func (b *chatworkBrowser) onSubmit(event js.Value) {
	form := event.Get("target").Call("closest", "form[data-chatwork-form]")
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
	view, key, conversation := b.view, b.key, b.conversationID
	switch {
	case strings.TrimSpace(view.Title) == "":
		b.view.Error = "title"
	case view.ProjectID == "":
		b.view.Error = "project"
	default:
		b.view.Error, b.view.Busy = "", true
	}
	invalid := !b.view.Busy
	generation := b.generation
	b.mu.Unlock()
	b.render(false)
	if invalid {
		field := "#chatwork-field-title"
		if view.ProjectID == "" && strings.TrimSpace(view.Title) != "" {
			field = "#chatwork-field-project"
		}
		if target := mount.Call("querySelector", field); target.Truthy() {
			target.Call("focus")
		}
		return
	}
	command := chatworkConvert{ConversationID: conversation, Target: "TASK", ProjectID: view.ProjectID, Title: strings.TrimSpace(view.Title), OwnerID: view.OwnerID, DueDate: view.DueDay, IdempotencyKey: key}
	go func() {
		ctx, cancel := b.context()
		defer cancel()
		link, err := chatworkConvertMessage(ctx, http.DefaultClient, b.cfg, view.PostID, command)
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
					// The answer may have been lost after the task was made:
					// read what the message became, so the form can show it.
					go b.loadLinks(conversation)
				}
				return
			}
			if b.linkedConv == conversation {
				b.links = chatworkMerge(b.links, link)
			}
			b.mu.Unlock()
			b.closeDialog(true)
			b.announce(chatui.MessageWorkDoneText(b.cfg.Locale) + ": " + link.Title)
			go b.loadLinks(conversation)
			refreshChatRoute()
		})
	}()
}

func chatworkMerge(links map[string][]chatui.MessageWorkLink, link chatworkLinkJSON) map[string][]chatui.MessageWorkLink {
	next := make(map[string][]chatui.MessageWorkLink, len(links)+1)
	for post, list := range links {
		next[post] = list
	}
	for _, existing := range next[link.PostID] {
		if existing.TaskID == link.TaskID {
			return next
		}
	}
	next[link.PostID] = append(append([]chatui.MessageWorkLink(nil), next[link.PostID]...), chatworkByPost([]chatworkLinkJSON{link})[link.PostID]...)
	return next
}

// announce tells a screen reader that the task was made.
func (b *chatworkBrowser) announce(text string) {
	doc := js.Global().Get("document")
	live := doc.Call("querySelector", "[data-chatwork-live]")
	if !live.Truthy() {
		live = doc.Call("createElement", "div")
		live.Call("setAttribute", "data-chatwork-live", "true")
		live.Call("setAttribute", "role", "status")
		live.Call("setAttribute", "aria-live", "polite")
		live.Call("setAttribute", "style", "position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap")
		doc.Get("body").Call("appendChild", live)
	}
	live.Set("textContent", text)
}
