//go:build js && wasm

package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3782: the browser half of the evidence attached to a conversation's
// case. Once the page knows the open conversation is bound to a case, it asks
// the server for the evidence that reader may see, keeps the last word per
// conversation, and sends the attach form. The server decides everything; a
// conversation the server did not describe shows nothing, and every read is
// asked again after a while, so an edit or deletion of an original shows
// without a reload. The page sends message ids, never text.

const evidenceStale = 30 * time.Second

type evidenceBrowser struct {
	mu       sync.Mutex
	cfg      journeyclient.Config
	disposed bool
	states   map[string]chatui.EvidenceState
	askedAt  map[string]time.Time
	loading  map[string]bool
	busy     map[string]bool
	notice   map[string]string
	submit   js.Func
}

var evidenceActive *evidenceBrowser

func installCaseEvidence(cfg journeyclient.Config) func() {
	cfg = personaChatHTTPConfig(cfg)
	b := &evidenceBrowser{cfg: cfg, states: map[string]chatui.EvidenceState{}, askedAt: map[string]time.Time{}, loading: map[string]bool{}, busy: map[string]bool{}, notice: map[string]string{}}
	evidenceActive = b
	doc := js.Global().Get("document")
	b.submit = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && !b.disposed {
			b.onSubmit(args[0])
		}
		return nil
	})
	doc.Call("addEventListener", "submit", b.submit)
	return func() {
		b.disposed = true
		doc.Call("removeEventListener", "submit", b.submit)
		b.submit.Release()
		if evidenceActive == b {
			evidenceActive = nil
		}
	}
}

// evidenceProject gives the page's model what is known of the open
// conversation's case evidence. It asks only for a conversation the case
// surface says is bound, and drops what it knew when the binding is gone or
// names another case.
func evidenceProject(model chatui.Model, _ journeyclient.Config) chatui.Model {
	b := evidenceActive
	if b == nil || b.disposed || model.SelectedID == "" {
		return model
	}
	conversation := model.SelectedID
	boundHere := model.Case.Loaded && model.Case.Bound && model.Case.ConversationID == conversation && model.Case.CaseRef != ""
	b.mu.Lock()
	if !boundHere {
		delete(b.states, conversation)
		delete(b.askedAt, conversation)
		b.mu.Unlock()
		return model
	}
	state, known := b.states[conversation]
	if known && state.CaseRef != model.Case.CaseRef {
		// The case changed under the page: what it knew belongs to the old one.
		delete(b.states, conversation)
		delete(b.askedAt, conversation)
		known = false
	}
	asked, askedBefore := b.askedAt[conversation]
	load := !b.loading[conversation] && !b.busy[conversation] && (!askedBefore || time.Since(asked) > evidenceStale)
	if load {
		b.loading[conversation] = true
	}
	if known {
		state.Busy, state.Notice = b.busy[conversation], b.notice[conversation]
		model.Evidence = state
	}
	b.mu.Unlock()
	if load {
		go b.load(conversation)
	}
	return model
}

func (b *evidenceBrowser) load(conversation string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	state, err := evidenceRead(ctx, http.DefaultClient, b.cfg, conversation)
	ui.PostAsync(func() {
		b.mu.Lock()
		delete(b.loading, conversation)
		b.askedAt[conversation] = time.Now()
		if err == nil {
			b.states[conversation] = state
		} else {
			// An unreadable answer shows nothing it did not say.
			delete(b.states, conversation)
		}
		b.mu.Unlock()
		refreshChatRoute()
	})
}

func (b *evidenceBrowser) onSubmit(event js.Value) {
	form := event.Get("target")
	if !form.Truthy() || form.Get("hasAttribute").Type() != js.TypeFunction || !form.Call("hasAttribute", "data-evidence-form").Bool() {
		return
	}
	event.Call("preventDefault")
	var ids []string
	boxes := form.Call("querySelectorAll", "[name=message]")
	for i := 0; i < boxes.Get("length").Int(); i++ {
		box := boxes.Index(i)
		if box.Get("checked").Bool() {
			ids = append(ids, box.Get("value").String())
		}
	}
	field := func(name string) js.Value { return form.Call("querySelector", "[name="+name+"]") }
	revision, note, reviewed := field("case_revision"), field("note"), field("reviewed")
	if !revision.Truthy() || !note.Truthy() || !reviewed.Truthy() {
		return
	}
	confirmed := reviewed.Get("checked").Bool()
	b.attachFor(strings.TrimSpace(revision.Get("value").String()), strings.TrimSpace(note.Get("value").String()), ids, confirmed, form)
}

// attachFor sends the reviewed attachment for the open conversation. The case
// is the one the page showed the person, so the server can refuse when the
// conversation has moved to another.
func (b *evidenceBrowser) attachFor(revision, note string, ids []string, confirmed bool, form js.Value) {
	conversation := chatBrowser.snapshot().SelectedID
	if conversation == "" || b.disposed {
		return
	}
	b.mu.Lock()
	state := b.states[conversation]
	if b.busy[conversation] || state.CaseRef == "" {
		b.mu.Unlock()
		return
	}
	if len(ids) == 0 || !confirmed {
		b.notice[conversation] = "invalid"
		b.mu.Unlock()
		refreshChatRoute()
		return
	}
	caseRef := state.CaseRef
	b.busy[conversation], b.notice[conversation] = true, ""
	b.mu.Unlock()
	refreshChatRoute()
	go b.send(conversation, form, func(ctx context.Context) (chatui.EvidenceState, error) {
		return evidenceAttach(ctx, http.DefaultClient, b.cfg, conversation, caseRef, revision, note, ids)
	})
}

func (b *evidenceBrowser) send(conversation string, form js.Value, call func(context.Context) (chatui.EvidenceState, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	state, err := call(ctx)
	ui.PostAsync(func() {
		b.mu.Lock()
		delete(b.busy, conversation)
		var refused *caseError
		switch {
		case err == nil:
			b.states[conversation] = state
			b.askedAt[conversation] = time.Now()
			b.notice[conversation] = "done"
		case errors.As(err, &refused) && refused.Code == "permission_denied":
			b.notice[conversation] = "denied"
		case errors.As(err, &refused) && refused.Code == "invalid_request":
			b.notice[conversation] = "invalid"
		case errors.As(err, &refused) && refused.Code == "case_changed":
			// The case moved on: what the page knew is stale; ask again.
			delete(b.states, conversation)
			delete(b.askedAt, conversation)
			b.notice[conversation] = "changed"
		case errors.As(err, &refused) && refused.Code == "not_found":
			// A message is gone or no longer open to the person; ask again so the
			// list the person chooses from is current.
			delete(b.askedAt, conversation)
			b.notice[conversation] = "gone"
		default:
			b.notice[conversation] = "failed"
		}
		b.mu.Unlock()
		if err == nil {
			if form.Truthy() && form.Get("reset").Type() == js.TypeFunction {
				form.Call("reset")
			}
			b.announce(chatui.Issue3782Text(b.cfg.Locale, chatui.Issue3782DoneKey))
		}
		refreshChatRoute()
	})
}

// announce tells a screen reader that the evidence was attached.
func (b *evidenceBrowser) announce(text string) {
	doc := js.Global().Get("document")
	live := doc.Call("querySelector", "[data-chatevid-live]")
	if !live.Truthy() {
		live = doc.Call("createElement", "div")
		live.Call("setAttribute", "data-chatevid-live", "true")
		live.Call("setAttribute", "role", "status")
		live.Call("setAttribute", "aria-live", "polite")
		live.Call("setAttribute", "style", "position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap")
		doc.Get("body").Call("appendChild", live)
	}
	live.Set("textContent", text)
}
