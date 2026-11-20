//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3804: the browser half of what the agents of a channel remember. It
// reads the open conversation's notes (again whenever Conversation details
// opens, and after every change), proposes a message from its menu, and
// approves and forgets notes from the details section. Nothing is shown from
// memory that the server has not just said; a conversation the server will not
// answer for (a direct message, another company's) offers nothing.

const memoryStale = time.Minute

type memoryBrowser struct {
	mu       sync.Mutex
	cfg      journeyclient.Config
	disposed bool
	// The notes of loadedConv as the server last gave them to this reader.
	items       []memoryJSON
	canRemember bool
	isManager   bool
	loadedConv  string
	loadedOK    bool
	loadedAt    time.Time
	loading     string
	failedConv  string
	failedAt    time.Time
	failCode    string
	wasShown    bool
	// What the last action said, and which note is being changed.
	notice, errCode, busy string
	focusHeading          bool
}

var memoryActive *memoryBrowser

func installMemory(cfg journeyclient.Config) func() {
	cfg = personaChatHTTPConfig(cfg)
	b := &memoryBrowser{cfg: cfg}
	memoryActive = b
	return func() {
		b.disposed = true
		if memoryActive == b {
			memoryActive = nil
		}
	}
}

// memoryProject gives the page's model the notes of the open conversation. The
// section and the Remember row are offered only once the server has answered a
// read for this conversation, so there is never a control that cannot work.
func memoryProject(model chatui.Model, _ journeyclient.Config) chatui.Model {
	b := memoryActive
	if b == nil || b.disposed {
		return model
	}
	you, unknown := chatui.MemoryOwnerLabels(b.cfg.Locale)
	b.mu.Lock()
	selected := model.SelectedID
	opened := model.ShowDetails && !b.wasShown
	b.wasShown = model.ShowDetails
	same := b.loadedConv == selected
	state := chatui.MemoryState{Remember: b.remember, Approve: b.approve, Forget: b.forget, Refresh: b.refresh}
	switch {
	case selected == "":
	case same && b.loadedOK:
		state.Enabled, state.State = true, "ready"
		state.CanRemember, state.IsManager = b.canRemember, b.isManager
		state.Items = memoryItems(model, b.items, you, unknown)
		state.Busy, state.Notice, state.Error = b.busy, b.notice, b.errCode
	case b.failedConv == selected && b.failCode != "" && b.failCode != "invalid_argument" && b.failCode != "permission_denied" && b.failCode != "not_found":
		// A read that failed shows an unknown state with Try again, never an empty list.
		state.Enabled, state.State, state.Error = true, "error", b.failCode
	}
	want := selected != "" && b.loading != selected &&
		(!same || !b.loadedOK && (b.failedConv != selected || time.Since(b.failedAt) > 30*time.Second) || opened || same && time.Since(b.loadedAt) > memoryStale && model.ShowDetails)
	if want {
		b.loading = selected
	}
	b.mu.Unlock()
	model.Memory = state
	if want {
		go b.load(selected)
	}
	return model
}

func (b *memoryBrowser) host(conversation string) string {
	return channelTodoHost(conversation, b.cfg.Tenant)
}

func (b *memoryBrowser) load(conversation string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	reply, err := memoryCall(ctx, http.DefaultClient, b.cfg, "list", memoryCommand{HostTenantID: b.host(conversation), ConversationID: conversation})
	ui.PostAsync(func() {
		b.mu.Lock()
		if b.loading == conversation {
			b.loading = ""
		}
		if err != nil {
			b.failedConv, b.failedAt, b.failCode = conversation, time.Now(), memoryErrorCode(err)
			if b.loadedConv == conversation {
				b.loadedOK = false
			}
		} else {
			// A conversation that offers no notes answers with CanRemember false
			// and no items; the section is still drawn for a channel (empty) and
			// only a refusal hides it.
			b.items, b.canRemember, b.isManager = reply.Items, reply.CanRemember, reply.IsManager
			b.loadedConv, b.loadedOK, b.loadedAt, b.failedConv, b.failCode = conversation, true, time.Now(), "", ""
		}
		focus := b.focusHeading
		b.focusHeading = false
		b.mu.Unlock()
		refreshChatRoute()
		if focus {
			memoryFocusHeading()
		}
	})
}

func memoryFocusHeading() {
	ui.PostAsync(func() {
		if heading := js.Global().Get("document").Call("getElementById", "chat-details-memory-title"); heading.Truthy() {
			heading.Call("focus")
		}
	})
}

func (b *memoryBrowser) refresh() {
	model := chatBrowser.snapshot()
	if model.SelectedID == "" {
		return
	}
	b.mu.Lock()
	b.failedConv, b.failCode = "", ""
	b.loading = model.SelectedID
	b.mu.Unlock()
	go b.load(model.SelectedID)
}

// act runs one change and then reads the notes again, so what the person sees
// afterwards is what the server holds. what is "proposed", "approved" or "forgot".
func (b *memoryBrowser) act(action, id, what string, focusAfter bool) {
	model := chatBrowser.snapshot()
	conversation := model.SelectedID
	if conversation == "" || b.disposed {
		return
	}
	b.mu.Lock()
	if b.busy != "" {
		b.mu.Unlock()
		return
	}
	b.busy, b.notice, b.errCode = id, "", ""
	if action == "propose" {
		b.busy = "remember"
	}
	b.mu.Unlock()
	refreshChatRoute()
	command := memoryCommand{HostTenantID: b.host(conversation), ConversationID: conversation}
	if action == "propose" {
		command.SourcePostID = id
	} else {
		command.ID = id
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := memoryCall(ctx, http.DefaultClient, b.cfg, action, command)
		ui.PostAsync(func() {
			b.mu.Lock()
			b.busy = ""
			if err != nil {
				b.errCode = memoryErrorCode(err)
			} else {
				b.notice = chatui.MemoryDoneText(b.cfg.Locale, what)
				b.focusHeading = focusAfter
			}
			b.loading = conversation
			b.mu.Unlock()
			go b.load(conversation)
		})
	}()
}

func (b *memoryBrowser) remember(postID string) { b.act("propose", postID, "proposed", false) }
func (b *memoryBrowser) approve(id string)      { b.act("approve", id, "approved", false) }
func (b *memoryBrowser) forget(id string)       { b.act("forget", id, "forgot", true) }
