//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"syscall/js"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type chatattach001Request struct {
	xhr       js.Value
	callbacks []js.Func
}
type chatattach001Bridge struct {
	state     *chatattach001State
	cfg       journeyclient.Config
	listeners map[string]js.Func
	pending   map[string]*chatattach001Request
	// kept holds the chosen file of every draft attachment that is not yet
	// uploaded, so an upload that failed to get through can be sent again
	// without the person choosing the file a second time.
	kept     map[string]js.Value
	disposed bool
}

func chatattach001Dispose() {
	chatBrowser.mu.Lock()
	old := chatBrowser.chatattach001
	chatBrowser.chatattach001 = nil
	chatBrowser.mu.Unlock()
	if old != nil && old.dispose != nil {
		old.dispose()
	}
}

func chatattach001Configure(cfg journeyclient.Config) {
	chatattach001Dispose()
	b := &chatattach001Bridge{state: &chatattach001State{}, cfg: cfg, pending: map[string]*chatattach001Request{}, kept: map[string]js.Value{}, listeners: map[string]js.Func{}}
	b.state.choose = func() { b.choose(chatui.Chatattach001PickerID) }
	b.state.chooseThread = func() { b.choose(chatui.Chatattach001ThreadPickerID) }
	b.state.remove = b.remove
	b.state.send = func(scope, body string, refs []chatui.ChatReference) { go b.send(scope, body, refs) }
	b.state.dispose = b.dispose
	chatBrowser.mu.Lock()
	chatBrowser.chatattach001 = b.state
	chatBrowser.mu.Unlock()
	// Nothing is offered until the server says it takes uploads.
	b.askAvailability()
	for _, name := range []string{"click", "change", "paste", "drop", "dragover", "input"} {
		kind := name
		f := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if !b.disposed && len(args) > 0 {
				b.event(kind, args[0])
			}
			return nil
		})
		b.listeners[kind] = f
		js.Global().Get("document").Call("addEventListener", kind, f)
	}
}

func (b *chatattach001Bridge) current() bool {
	chatBrowser.mu.RLock()
	defer chatBrowser.mu.RUnlock()
	return !b.disposed && chatBrowser.chatattach001 == b.state && chatBrowser.cfg.Tenant == b.cfg.Tenant && chatBrowser.cfg.Subject == b.cfg.Subject
}

func (b *chatattach001Bridge) choose(picker string) {
	if !b.current() || !b.state.takes() {
		return
	}
	field := js.Global().Get("document").Call("getElementById", picker)
	if field.Truthy() {
		field.Set("value", "")
		field.Call("click")
	}
}

// askAvailability asks the server once whether it takes uploads. Any answer
// but a plain yes leaves the page as it starts: with no way to attach a file.
func (b *chatattach001Bridge) askAvailability() {
	xhr := js.Global().Get("XMLHttpRequest").New()
	var done js.Func
	done = js.FuncOf(func(js.Value, []js.Value) any {
		defer done.Release()
		xhr.Set("onloadend", js.Null())
		if b.disposed {
			return nil
		}
		if chatattach001Available(xhr.Get("status").Int(), xhr.Get("responseText").String()) {
			b.state.setAvailable(true)
			chatStreamRender.Schedule()
		}
		return nil
	})
	xhr.Set("onloadend", done)
	xhr.Call("open", "GET", chatattach001AvailabilityRoute)
	xhr.Call("setRequestHeader", "Authorization", "Bearer "+b.cfg.Bearer)
	xhr.Call("send")
}

// scopeOf is the scope a control of the page belongs to: the open thread's
// when it is in the reply box, else the conversation's.
func (b *chatattach001Bridge) scopeOf(node js.Value) string {
	room := chatBrowser.selectedID()
	if node.Truthy() && node.Get("closest").Type() == js.TypeFunction && node.Call("closest", ".thread-composer").Truthy() {
		return chatattach001Scope(room, chatattach001ThreadParent())
	}
	return room
}

// chatattach001ThreadParent is the message the open thread hangs under.
func chatattach001ThreadParent() string {
	chatBrowser.mu.RLock()
	defer chatBrowser.mu.RUnlock()
	if !chatBrowser.model.ShowThread {
		return ""
	}
	return chatBrowser.model.ThreadParentID
}

func (b *chatattach001Bridge) event(kind string, e js.Value) {
	if !b.current() {
		return
	}
	target := e.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	if kind == "click" {
		if button := target.Call("closest", `[data-action="composer-add"][data-extra="attachment"]`); button.Truthy() {
			e.Call("preventDefault")
			b.choose(chatui.Chatattach001PickerID)
			return
		}
		if button := target.Call("closest", "[data-chatattach001-choose]"); button.Truthy() {
			// The reply box's paperclip.
			e.Call("preventDefault")
			b.choose(chatui.Chatattach001ThreadPickerID)
			return
		}
		if button := target.Call("closest", "[data-chatattach001-remove]"); button.Truthy() {
			e.Call("preventDefault")
			b.remove(b.scopeOf(button), button.Call("getAttribute", "data-chatattach001-remove").String())
			b.syncSendSoon()
			return
		}
		if button := target.Call("closest", "[data-chatattach001-retry]"); button.Truthy() {
			e.Call("preventDefault")
			b.retry(b.scopeOf(button), button.Call("getAttribute", "data-chatattach001-retry").String())
			return
		}
		return
	}
	if !target.Call("closest", ".chat-composer,.thread-composer").Truthy() {
		return
	}
	if kind == "input" {
		// The page's own handler sets Send from the text alone; this runs with
		// it and corrects it for the files.
		b.syncSendSoon()
		return
	}
	if !b.state.takes() {
		// No uploads here: a pasted or dropped file is left to the browser.
		return
	}
	var files js.Value
	switch kind {
	case "change":
		if id := target.Get("id").String(); id != chatui.Chatattach001PickerID && id != chatui.Chatattach001ThreadPickerID {
			return
		}
		files = target.Get("files")
	case "paste":
		data := e.Get("clipboardData")
		if !data.Truthy() {
			return
		}
		files = data.Get("files")
	case "drop", "dragover":
		data := e.Get("dataTransfer")
		if !data.Truthy() {
			return
		}
		if kind == "dragover" {
			if data.Get("types").Call("includes", "Files").Bool() {
				e.Call("preventDefault")
				data.Set("dropEffect", "copy")
			}
			return
		}
		files = data.Get("files")
	}
	if !files.Truthy() || files.Length() == 0 {
		return
	}
	e.Call("preventDefault")
	scope := b.scopeOf(target)
	if scope == "" {
		return
	}
	b.state.clearError(scope)
	for i := 0; i < files.Length(); i++ {
		b.upload(scope, files.Index(i))
	}
	chatStreamRender.Schedule()
	b.syncSend()
}

// syncSend sets each composer's Send button for the files it holds: off while
// one is uploading or failed, and on for uploaded files with no text, which the
// page's own input handler would leave off.
func (b *chatattach001Bridge) syncSend() {
	if b.disposed {
		return
	}
	document := js.Global().Get("document")
	view := b.state.projectionFor(chatBrowser.selectedID(), chatattach001ThreadParent())
	for selector, files := range map[string]*chatui.Chatattach001Composer{".chat-composer": view, ".thread-composer": view.Thread} {
		form := document.Call("querySelector", selector)
		if !form.Truthy() || files == nil {
			continue
		}
		button := form.Call("querySelector", ".send-button")
		field := form.Call("querySelector", ".composer-input")
		if !button.Truthy() || !field.Truthy() {
			continue
		}
		state := chatattach001SendState(form.Call("getAttribute", "data-send-capable").String() == "true", field.Get("value").String(), len(files.Files), files.Ready())
		if state == chatattach001SendLeave {
			continue
		}
		button.Set("disabled", state == chatattach001SendOff)
		aria := "false"
		if state == chatattach001SendOff {
			aria = "true"
		}
		button.Call("setAttribute", "aria-disabled", aria)
	}
}

// syncSendSoon runs syncSend now and again once the handlers of the event in
// hand have run, so it has the last word whatever their order.
func (b *chatattach001Bridge) syncSendSoon() {
	b.syncSend()
	var later js.Func
	later = js.FuncOf(func(js.Value, []js.Value) any {
		later.Release()
		b.syncSend()
		return nil
	})
	js.Global().Call("queueMicrotask", later)
}

func (b *chatattach001Bridge) upload(room string, file js.Value) {
	if room == "" || !b.current() || !b.state.takes() {
		return
	}
	name, kind, size := file.Get("name").String(), file.Get("type").String(), int64(file.Get("size").Float())
	if kind == "" {
		kind = chatMediaContentType(name)
	}
	preview := ""
	if strings.HasPrefix(kind, "image/") && size <= chatattach001MaxBytes {
		preview = js.Global().Get("URL").Call("createObjectURL", file).String()
	}
	key, ok := b.state.add(room, name, kind, preview, size)
	if !ok {
		if preview != "" {
			js.Global().Get("URL").Call("revokeObjectURL", preview)
		}
		return
	}
	b.kept[key] = file
	b.start(room, key, file, name, preview)
}

// retry sends a file whose upload did not get through once more.
func (b *chatattach001Bridge) retry(room, key string) {
	file, ok := b.kept[key]
	if !ok || !b.current() || !b.state.retry(room, key) {
		return
	}
	b.start(room, key, file, file.Get("name").String(), b.state.preview(room, key))
	chatStreamRender.Schedule()
	b.syncSend()
}

// start posts one draft attachment's file to the media service and follows it
// to its end: uploaded, refused (the draft loses it and says why) or not
// through (the draft keeps it for Retry).
func (b *chatattach001Bridge) start(room, key string, file js.Value, name, preview string) {
	xhr := js.Global().Get("XMLHttpRequest").New()
	req := &chatattach001Request{xhr: xhr}
	b.pending[key] = req
	progress := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if b.current() && len(args) > 0 && args[0].Get("lengthComputable").Bool() {
			e := args[0]
			total := e.Get("total").Float()
			if total > 0 {
				b.state.progress(room, key, int(e.Get("loaded").Float()*100/total))
				chatStreamRender.Schedule()
			}
		}
		return nil
	})
	done := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		delete(b.pending, key)
		if b.current() {
			var ref struct {
				ArtifactID, TenantID, ConversationID, MediaType string
				Size                                            int64
				State                                           string
			}
			status := xhr.Get("status").Int()
			if status >= 200 && status < 300 {
				if conversation, _ := chatattach001ScopeParts(room); json.Unmarshal([]byte(xhr.Get("responseText").String()), &ref) != nil || ref.TenantID != b.cfg.Tenant || ref.ConversationID != conversation || ref.State != "ADMITTED" {
					status = 500
				}
			}
			switch b.state.complete(room, key, ref.ArtifactID, ref.MediaType, ref.Size, status) {
			case chatattach001Uploaded:
				delete(b.kept, key)
			case chatattach001Dropped:
				delete(b.kept, key)
				if preview != "" {
					js.Global().Get("URL").Call("revokeObjectURL", preview)
				}
			}
			chatStreamRender.Schedule()
			b.syncSend()
		}
		xhr.Set("onloadend", js.Null())
		xhr.Get("upload").Set("onprogress", js.Null())
		for _, f := range req.callbacks {
			f.Release()
		}
		return nil
	})
	req.callbacks = []js.Func{progress, done}
	xhr.Get("upload").Set("onprogress", progress)
	xhr.Set("onloadend", done)
	conversation, _ := chatattach001ScopeParts(room)
	query := url.Values{"host_tenant_id": {b.cfg.Tenant}, "conversation_id": {conversation}}
	xhr.Call("open", "POST", chatMediaRoute+"upload?"+query.Encode())
	xhr.Call("setRequestHeader", "Authorization", "Bearer "+b.cfg.Bearer)
	form := js.Global().Get("FormData").New()
	form.Call("append", "file", file, name)
	xhr.Call("send", form)
}

func (b *chatattach001Bridge) remove(room, key string) {
	raw := b.state.drop(room, key)
	delete(b.kept, key)
	if request := b.pending[key]; request != nil {
		request.xhr.Call("abort")
	}
	if raw != "" {
		js.Global().Get("URL").Call("revokeObjectURL", raw)
	}
	chatStreamRender.Schedule()
}

func (b *chatattach001Bridge) dispose() {
	b.disposed = true
	for name, f := range b.listeners {
		js.Global().Get("document").Call("removeEventListener", name, f)
		f.Release()
	}
	for _, request := range b.pending {
		request.xhr.Call("abort")
	}
	b.state.mu.Lock()
	for _, r := range b.state.rooms {
		for _, f := range r.files {
			if f.URL != "" {
				js.Global().Get("URL").Call("revokeObjectURL", f.URL)
			}
		}
	}
	b.state.rooms = nil
	b.state.mu.Unlock()
}

// send posts a scope's files with the text that goes with them, which may be
// none: a message in the conversation, or a reply under the scope's parent. A
// send that fails keeps the files and gives the text back to the box it came
// from.
func (b *chatattach001Bridge) send(scope, body string, mentions []chatui.ChatReference) {
	room, parent := chatattach001ScopeParts(scope)
	if !b.current() || chatBrowser.selectedID() != room {
		return
	}
	refs, identity, ok := b.state.begin(scope, b.cfg.Tenant, body, mentions)
	if !ok {
		return
	}
	chatStreamRender.Schedule()
	active := chatBrowser.config(b.cfg)
	authorKey, what := chatui.ModAuthorKeyComposer(room), "send this message"
	if parent != "" {
		authorKey, what = chatui.ModAuthorKeyReply(parent), "post this reply"
	}
	restore := func() {
		if parent == "" {
			chatBrowser.restoreDraft(room, body)
			flushChatDraftPersist(active)
			return
		}
		// The reply box's text lives in the page, not in the stored drafts.
		if field := js.Global().Get("document").Call("getElementById", "thread-composer"); field.Truthy() && field.Get("value").String() == "" {
			field.Set("value", body)
		}
	}
	client := chatBrowser.conversationClient()
	if client == nil {
		b.state.finish(scope, false)
		restore()
		chatStreamRender.Schedule()
		b.syncSend()
		return
	}
	keyTarget := "chatattach001:" + scope
	key := chatBrowser.sendKey(keyTarget, identity, func() string { return fmt.Sprintf("attachment-send-%d", time.Now().UnixNano()) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	callCtx := chatRPCContext(ctx, active)
	sent, err := client.SendPost(callCtx, &chatv1.SendPostRequest{TenantId: active.Tenant, ConversationId: room, Body: body, ParentId: parent, References: refs, IdempotencyKey: key})
	if !b.current() {
		return
	}
	if err != nil {
		b.state.finish(scope, false)
		restore()
		chatmod002Failed(what, err, room, authorKey, chatui.ModAuthorSurfaceMessage, body)
		chatStreamRender.Schedule()
		b.syncSend()
		return
	}
	for _, raw := range b.state.finish(scope, true) {
		js.Global().Get("URL").Call("revokeObjectURL", raw)
	}
	chatBrowser.clearSendKey(keyTarget)
	chatBrowser.applySentChatPost(room, sent.GetPost(), active.Locale, chatDirectorySnapshot(), time.Now())
	if parent != "" {
		refreshChatThread(callCtx, client, active, room, parent)
	}
	chatStreamRender.Schedule()
	b.syncSend()
}
