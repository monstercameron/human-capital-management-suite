//go:build js && wasm

package main

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3793: the browser half of typed requests (/leave). The page asks the
// server which request commands this person may use in the open conversation,
// opens the panel when /leave is chosen, checks the typed values with the
// server (Review), and sends the reviewed request (Send). The server decides
// everything; this keeps the person's values, shows the server's word, and
// holds one submission key for one reviewed request: a retry after an uncertain
// failure reuses it, and editing the request starts a new one.

const requestStale = 60 * time.Second

type requestBrowser struct {
	mu       sync.Mutex
	cfg      journeyclient.Config
	disposed bool
	// conversation is the one the commands below were read for.
	conversation string
	commands     map[string]chatui.IntentRequestAvailable
	loadedAt     time.Time
	loading      bool
	view         chatui.IntentRequestView
	// submission names the reviewed request now in the panel; empty until the
	// server has accepted a review.
	submission string
	working    bool
	submit     js.Func
	click      js.Func
	keydown    js.Func
}

var requestActive *requestBrowser

func installIntentRequests(cfg journeyclient.Config) func() {
	cfg = personaChatHTTPConfig(cfg)
	b := &requestBrowser{cfg: cfg, commands: map[string]chatui.IntentRequestAvailable{}}
	requestActive = b
	doc := js.Global().Get("document")
	b.submit = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && !b.disposed {
			b.onSubmit(args[0])
		}
		return nil
	})
	b.click = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && !b.disposed {
			b.onClick(args[0])
		}
		return nil
	})
	b.keydown = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && !b.disposed {
			b.onKey(args[0])
		}
		return nil
	})
	doc.Call("addEventListener", "submit", b.submit)
	doc.Call("addEventListener", "click", b.click)
	doc.Call("addEventListener", "keydown", b.keydown)
	return func() {
		b.disposed = true
		doc.Call("removeEventListener", "submit", b.submit)
		doc.Call("removeEventListener", "click", b.click)
		doc.Call("removeEventListener", "keydown", b.keydown)
		b.submit.Release()
		b.click.Release()
		b.keydown.Release()
		if requestActive == b {
			requestActive = nil
		}
	}
}

// requestProject gives the page's model what is known of the typed requests in
// the open conversation, and starts the read when it is missing or stale.
func requestProject(model chatui.Model, _ journeyclient.Config) chatui.Model {
	b := requestActive
	if b == nil || b.disposed || model.SelectedID == "" {
		return model
	}
	conversation := model.SelectedID
	read := false
	b.mu.Lock()
	if b.conversation != conversation {
		// Another conversation: nothing known about it yet, and a request open
		// in the last one does not follow the person.
		b.conversation, b.commands, b.loadedAt, b.loading = conversation, map[string]chatui.IntentRequestAvailable{}, time.Time{}, false
		if b.view.ConversationID != conversation {
			b.view, b.submission = chatui.IntentRequestView{}, ""
		}
	}
	if !b.loading && !b.working && time.Since(b.loadedAt) > requestStale {
		b.loading, read = true, true
	}
	known := len(b.commands) > 0
	model.IntentRequests.Commands = map[string]chatui.IntentRequestAvailable{}
	for name, state := range b.commands {
		model.IntentRequests.Commands[name] = state
	}
	if known {
		model.IntentRequests.ConversationID = conversation
		model.IntentRequests.Open = b.open
	}
	if b.view.Open && b.view.ConversationID == conversation {
		model.IntentRequests.View = b.view
	}
	b.mu.Unlock()
	if read {
		go b.load(conversation)
	}
	return model
}

func (b *requestBrowser) load(conversation string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	commands, err := requestRead(ctx, http.DefaultClient, b.cfg, conversation)
	ui.PostAsync(func() {
		b.mu.Lock()
		b.loading = false
		b.loadedAt = time.Now()
		// An unreadable answer is not "nothing is installed": keep what was known.
		if err == nil && b.conversation == conversation {
			b.commands = commands
		}
		b.mu.Unlock()
		refreshChatRoute()
	})
}

// open runs when /leave is chosen.
func (b *requestBrowser) open(command string) {
	conversation := chatBrowser.snapshot().SelectedID
	if conversation == "" {
		return
	}
	b.mu.Lock()
	b.view = chatui.IntentRequestView{Open: true, Command: command, ConversationID: conversation, Phase: chatui.IntentRequestForm, Values: map[string]string{}}
	b.submission, b.working = "", false
	b.mu.Unlock()
	refreshChatRoute()
	b.focus("#chat-request-panel [name]", "#chat-request-heading")
}

// focus moves focus to the first element that exists after the next draw.
func (b *requestBrowser) focus(selectors ...string) {
	ui.PostAsync(func() {
		doc := js.Global().Get("document")
		for _, selector := range selectors {
			if el := doc.Call("querySelector", selector); el.Truthy() {
				el.Call("focus")
				return
			}
		}
	})
}

func requestNewKey() string {
	return js.Global().Get("crypto").Call("randomUUID").String()
}

func (b *requestBrowser) onSubmit(event js.Value) {
	form := event.Get("target")
	if !form.Truthy() || form.Get("hasAttribute").Type() != js.TypeFunction || !form.Call("hasAttribute", "data-intent-request").Bool() {
		return
	}
	event.Call("preventDefault")
	values := map[string]string{}
	fields := form.Call("querySelectorAll", "[name]")
	for i := 0; i < fields.Length(); i++ {
		field := fields.Index(i)
		if name := field.Get("name").String(); name != "" {
			values[name] = field.Get("value").String()
		}
	}
	b.mu.Lock()
	if !b.view.Open || b.working || b.view.Phase != chatui.IntentRequestForm {
		b.mu.Unlock()
		return
	}
	command, conversation := b.view.Command, b.view.ConversationID
	b.view.Values, b.view.Errors, b.view.Notice = values, nil, ""
	b.working = true
	b.mu.Unlock()
	go b.review(conversation, command, values)
}

func (b *requestBrowser) review(conversation, command string, values map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	via, err := requestPreview(ctx, http.DefaultClient, b.cfg, conversation, command, values)
	ui.PostAsync(func() {
		b.mu.Lock()
		b.working = false
		focus := []string{"#chat-request-heading"}
		if b.view.ConversationID != conversation || !b.view.Open {
			b.mu.Unlock()
			return
		}
		var refused *requestError
		switch {
		case err == nil:
			b.view.Phase, b.view.Via = chatui.IntentRequestReview, via
			// One reviewed request, one submission key.
			b.submission = requestNewKey()
		case errors.As(err, &refused) && refused.Code == "invalid_request":
			b.view.Errors, focus = refused.Fields, []string{"#chat-request-panel [aria-invalid='true']"}
		case errors.As(err, &refused) && refused.Code == "not_available":
			b.commands[command] = chatui.IntentRequestAvailable{Loaded: true, Reason: refused.Reason}
		case errors.As(err, &refused) && refused.Code == "permission_denied":
			b.view.Notice, focus = "denied", []string{"#chat-request-notice"}
		default:
			b.view.Notice, focus = "failed", []string{"#chat-request-notice"}
		}
		b.mu.Unlock()
		refreshChatRoute()
		b.focus(focus...)
	})
}

func (b *requestBrowser) onClick(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	button := target.Call("closest", "[data-request-action]")
	if !button.Truthy() {
		return
	}
	event.Call("preventDefault")
	action := button.Get("dataset").Get("requestAction").String()
	b.mu.Lock()
	open := b.view.Open
	b.mu.Unlock()
	if !open {
		return
	}
	switch action {
	case "close":
		b.close()
	case "edit":
		b.mu.Lock()
		if !b.working {
			b.view.Phase, b.view.Notice = chatui.IntentRequestForm, ""
			// A different request is a different submission.
			b.submission = ""
		}
		b.mu.Unlock()
		refreshChatRoute()
		b.focus("#chat-request-panel [name]", "#chat-request-heading")
	case "send":
		b.send()
	}
}

func (b *requestBrowser) onKey(event js.Value) {
	if event.Get("key").String() != "Escape" {
		return
	}
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction || !target.Call("closest", "#chat-request-panel").Truthy() {
		return
	}
	b.mu.Lock()
	open := b.view.Open
	b.mu.Unlock()
	if open {
		event.Call("preventDefault")
		b.close()
	}
}

func (b *requestBrowser) close() {
	b.mu.Lock()
	b.view, b.submission, b.working = chatui.IntentRequestView{}, "", false
	b.mu.Unlock()
	refreshChatRoute()
	// Focus goes back to where the person was typing.
	b.focus("#chat-composer")
}

func (b *requestBrowser) send() {
	b.mu.Lock()
	if !b.view.Open || b.working || b.view.Phase != chatui.IntentRequestReview || b.submission == "" {
		b.mu.Unlock()
		return
	}
	conversation, command, key := b.view.ConversationID, b.view.Command, b.submission
	values := map[string]string{}
	for name, value := range b.view.Values {
		values[name] = value
	}
	b.view.Phase, b.view.Notice, b.working = chatui.IntentRequestSending, "", true
	b.mu.Unlock()
	refreshChatRoute()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		href, err := requestSend(ctx, http.DefaultClient, b.cfg, conversation, command, key, values)
		ui.PostAsync(func() {
			b.mu.Lock()
			b.working = false
			if b.view.ConversationID != conversation || !b.view.Open {
				b.mu.Unlock()
				return
			}
			focus := []string{"#chat-request-heading"}
			var refused *requestError
			switch {
			case err == nil:
				b.view.Phase, b.view.Href = chatui.IntentRequestSent, href
				b.submission = ""
			case errors.As(err, &refused) && refused.Code == "invalid_request":
				// Something changed since the review: back to the form.
				b.view.Phase, b.view.Errors, b.submission = chatui.IntentRequestForm, refused.Fields, ""
				focus = []string{"#chat-request-panel [aria-invalid='true']"}
			case errors.As(err, &refused) && refused.Code == "not_available":
				b.view.Phase, b.submission = chatui.IntentRequestForm, ""
				b.commands[command] = chatui.IntentRequestAvailable{Loaded: true, Reason: refused.Reason}
			case errors.As(err, &refused) && refused.Code == "permission_denied":
				b.view.Phase, b.view.Notice, focus = chatui.IntentRequestReview, "denied", []string{"#chat-request-notice"}
			default:
				// Not known whether it went through: stay on the review with the
				// same key, so pressing Send again recovers the same request
				// instead of making a second one.
				b.view.Phase, b.view.Notice, focus = chatui.IntentRequestReview, "failed", []string{"#chat-request-notice"}
			}
			b.mu.Unlock()
			refreshChatRoute()
			b.focus(focus...)
		})
	}()
}
