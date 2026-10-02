//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// The chat composition owns this value and its listeners. It requires neither
// storage, a timer, focus events nor a mutation observer to do its work.
type agentUXAmbientBrowser struct {
	mu            sync.Mutex
	cfg           journeyclient.Config
	state         agentUXAmbientClientState
	changed       func(ambientagents.Snapshot, bool)
	click, submit js.Func
}

func newAgentUXAmbientBrowser(cfg journeyclient.Config, changed func(ambientagents.Snapshot, bool)) *agentUXAmbientBrowser {
	b := &agentUXAmbientBrowser{cfg: cfg, changed: changed}
	b.click = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			b.handleClick(args[0])
		}
		return nil
	})
	b.submit = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			b.handleSubmit(args[0])
		}
		return nil
	})
	document := js.Global().Get("document")
	document.Call("addEventListener", "click", b.click)
	document.Call("addEventListener", "submit", b.submit)
	return b
}

func (b *agentUXAmbientBrowser) Close() {
	document := js.Global().Get("document")
	document.Call("removeEventListener", "click", b.click)
	document.Call("removeEventListener", "submit", b.submit)
	b.click.Release()
	b.submit.Release()
}
func (b *agentUXAmbientBrowser) Refresh(conversation string) {
	b.request(conversation, "", nil)
}
func (b *agentUXAmbientBrowser) HandleEvent(conversation string, event chat.ConversationEvent) {
	b.mu.Lock()
	current := b.state.Conversation == conversation
	b.mu.Unlock()
	if current && chat.AgentUXAmbientInvalidation(event) {
		b.Refresh(conversation)
	}
}
func (b *agentUXAmbientBrowser) request(conversation, action string, input any) {
	b.mu.Lock()
	generation := b.state.begin(conversation)
	command, control := input.(ambientagents.Command)
	if control {
		for i := range b.state.Snapshot.Cards {
			if b.state.Snapshot.Cards[i].ID == command.ID {
				b.state.Snapshot.Cards[i].Busy = true
				b.state.Snapshot.Cards[i].Error = false
			}
		}
	}
	before := b.state
	b.mu.Unlock()
	if b.changed != nil {
		b.changed(before.Snapshot, before.Failed)
	}
	go func() {
		snapshot, err := agentUXAmbientRequest(context.Background(), http.DefaultClient, b.cfg, conversation, action, input)
		b.mu.Lock()
		applied := b.state.finish(conversation, generation, snapshot, err)
		if applied && err != nil && control {
			for i := range b.state.Snapshot.Cards {
				if b.state.Snapshot.Cards[i].ID == command.ID {
					b.state.Snapshot.Cards[i].Busy = false
					b.state.Snapshot.Cards[i].Error = true
				}
			}
		}
		state := b.state
		b.mu.Unlock()
		if b.changed != nil && applied {
			b.changed(state.Snapshot, state.Failed)
		}
	}()
}

func (b *agentUXAmbientBrowser) handleClick(event js.Value) {
	target := event.Get("target")
	if target.IsNull() || target.IsUndefined() {
		return
	}
	button := target.Call("closest", "[data-ambient-action]")
	if button.IsNull() || button.IsUndefined() {
		return
	}
	event.Call("preventDefault")
	action := button.Call("getAttribute", "data-ambient-action").String()
	id := button.Call("getAttribute", "data-ambient-id").String()
	conversation := button.Call("getAttribute", "data-ambient-conversation").String()
	revision, _ := strconv.ParseUint(button.Call("getAttribute", "data-ambient-revision").String(), 10, 64)
	if action == "REFRESH" {
		b.Refresh(conversation)
		return
	}
	if action == "EDIT_TASK" || action == "EDIT_TIME" || action == "SNOOZE" || action == "CLOSE_EDIT" {
		b.mu.Lock()
		for index := range b.state.Snapshot.Cards {
			card := &b.state.Snapshot.Cards[index]
			if card.ID == id {
				card.Editing = action != "CLOSE_EDIT"
			}
		}
		state := b.state
		b.mu.Unlock()
		if b.changed != nil {
			b.changed(state.Snapshot, state.Failed)
		}
		return
	}
	b.request(conversation, "control", ambientagents.Command{Conversation: conversation, ID: id, Action: action, ExpectedRevision: revision})
}

func (b *agentUXAmbientBrowser) handleSubmit(event js.Value) {
	form := event.Get("target")
	if form.IsNull() || form.IsUndefined() || !form.Call("hasAttribute", "data-ambient-form").Bool() {
		return
	}
	event.Call("preventDefault")
	id := form.Call("getAttribute", "data-ambient-form").String()
	conversation := form.Call("getAttribute", "data-ambient-conversation").String()
	revision, _ := strconv.ParseUint(form.Call("getAttribute", "data-ambient-revision").String(), 10, 64)
	read := func(name string) string {
		input := form.Call("querySelector", "[name='"+name+"']")
		if input.IsNull() || input.IsUndefined() {
			return ""
		}
		return input.Get("value").String()
	}
	action := "EDIT"
	if form.Call("getAttribute", "data-ambient-kind").String() == "REMINDER" {
		action = "CHANGE"
	}
	b.request(conversation, "control", ambientagents.Command{Conversation: conversation, ID: id, Action: action, Title: read("title"), Date: read("date"), Clock: read("clock"), ExpectedRevision: revision})
}
