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
	disposed  bool
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
	b := &chatattach001Bridge{state: &chatattach001State{}, cfg: cfg, pending: map[string]*chatattach001Request{}, listeners: map[string]js.Func{}}
	b.state.choose = b.choose
	b.state.remove = b.remove
	b.state.send = func(room, body string, refs []chatui.ChatReference) { go b.send(room, body, refs) }
	b.state.dispose = b.dispose
	chatBrowser.mu.Lock()
	chatBrowser.chatattach001 = b.state
	chatBrowser.mu.Unlock()
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

func (b *chatattach001Bridge) choose() {
	if !b.current() {
		return
	}
	field := js.Global().Get("document").Call("getElementById", "chatattach001-picker")
	if field.Truthy() {
		field.Set("value", "")
		field.Call("click")
	}
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
			b.choose()
			return
		}
		if button := target.Call("closest", "[data-chatattach001-remove]"); button.Truthy() {
			e.Call("preventDefault")
			b.remove(chatBrowser.selectedID(), button.Call("getAttribute", "data-chatattach001-remove").String())
			return
		}
		return
	}
	if !target.Call("closest", ".chat-composer").Truthy() {
		return
	}
	if kind == "input" {
		b.disableSend()
		return
	}
	var files js.Value
	switch kind {
	case "change":
		if target.Get("id").String() != "chatattach001-picker" {
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
	room := chatBrowser.selectedID()
	for i := 0; i < files.Length(); i++ {
		b.upload(room, files.Index(i))
	}
	chatStreamRender.Schedule()
	b.disableSend()
}

func (b *chatattach001Bridge) disableSend() {
	room := chatBrowser.selectedID()
	ready := b.state.projection(room).Ready()
	form := js.Global().Get("document").Call("querySelector", ".chat-composer")
	if !form.Truthy() {
		return
	}
	if !ready {
		button := form.Call("querySelector", ".send-button")
		if button.Truthy() {
			button.Set("disabled", true)
			button.Call("setAttribute", "aria-disabled", "true")
		}
	}
}

func (b *chatattach001Bridge) upload(room string, file js.Value) {
	if room == "" || !b.current() {
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
				if json.Unmarshal([]byte(xhr.Get("responseText").String()), &ref) != nil || ref.TenantID != b.cfg.Tenant || ref.ConversationID != room || ref.State != "ADMITTED" {
					status = 500
				}
			}
			if !b.state.complete(room, key, ref.ArtifactID, ref.MediaType, ref.Size, status) && preview != "" {
				js.Global().Get("URL").Call("revokeObjectURL", preview)
			}
			chatStreamRender.Schedule()
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
	query := url.Values{"host_tenant_id": {b.cfg.Tenant}, "conversation_id": {room}}
	xhr.Call("open", "POST", chatMediaRoute+"upload?"+query.Encode())
	xhr.Call("setRequestHeader", "Authorization", "Bearer "+b.cfg.Bearer)
	form := js.Global().Get("FormData").New()
	form.Call("append", "file", file, name)
	xhr.Call("send", form)
}

func (b *chatattach001Bridge) remove(room, key string) {
	raw := b.state.drop(room, key)
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

func (b *chatattach001Bridge) send(room, body string, mentions []chatui.ChatReference) {
	if !b.current() || chatBrowser.selectedID() != room {
		return
	}
	refs, identity, ok := b.state.begin(room, b.cfg.Tenant, body, mentions)
	if !ok {
		return
	}
	chatStreamRender.Schedule()
	active := chatBrowser.config(b.cfg)
	client := chatBrowser.conversationClient()
	if client == nil {
		b.state.finish(room, false)
		chatBrowser.restoreDraft(room, body)
		chatStreamRender.Schedule()
		return
	}
	keyTarget := "chatattach001:" + room
	key := chatBrowser.sendKey(keyTarget, identity, func() string { return fmt.Sprintf("attachment-send-%d", time.Now().UnixNano()) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sent, err := client.SendPost(chatRPCContext(ctx, active), &chatv1.SendPostRequest{TenantId: active.Tenant, ConversationId: room, Body: body, References: refs, IdempotencyKey: key})
	if !b.current() {
		return
	}
	if err != nil {
		b.state.finish(room, false)
		chatBrowser.restoreDraft(room, body)
		flushChatDraftPersist(active)
		chatmod002Failed("send this message", err, room, chatui.ModAuthorKeyComposer(room), chatui.ModAuthorSurfaceMessage, body)
		chatStreamRender.Schedule()
		return
	}
	for _, raw := range b.state.finish(room, true) {
		js.Global().Get("URL").Call("revokeObjectURL", raw)
	}
	chatBrowser.clearSendKey(keyTarget)
	chatBrowser.applySentChatPost(room, sent.GetPost(), active.Locale, chatDirectorySnapshot(), time.Now())
	chatStreamRender.Schedule()
}
