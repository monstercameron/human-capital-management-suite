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

// setConfig keeps the credentials current: the sign-in can be renewed while the
// page lives, and the browser's requests must carry the renewed one.
func (b *agentUXAmbientBrowser) setConfig(cfg journeyclient.Config) {
	b.mu.Lock()
	b.cfg = cfg
	b.mu.Unlock()
}

// adopt takes an answer the page read itself as the state of the conversation,
// keeping the cards a person has open for editing, and returns what to show.
func (b *agentUXAmbientBrowser) adopt(conversation string, snapshot ambientagents.Snapshot) ambientagents.Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state.Conversation == conversation {
		snapshot.Cards = keepAmbientEditing(b.state.Snapshot.Cards, snapshot.Cards)
	}
	b.state.Conversation, b.state.Snapshot, b.state.Failed = conversation, snapshot, false
	return snapshot
}

func (b *agentUXAmbientBrowser) conversation() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state.Conversation
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
	if action == "GRANT" {
		// The administrator's "Reads every message here". The server checks the
		// administrator and the installation; this only carries the choice.
		agent := button.Call("getAttribute", "data-ambient-agent").String()
		enabled := button.Call("getAttribute", "data-ambient-enabled").String() == "true"
		b.request(conversation, "grant", ambientagents.GrantCommand{Conversation: conversation, Agent: agent, Enabled: enabled})
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
