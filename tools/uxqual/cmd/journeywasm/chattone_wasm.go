//go:build js && wasm

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

type chattoneRoomState struct {
	reply     chattoneHTTPReply
	loading   bool
	errorCode string
	expires   time.Time
}
type chattoneBrowserController struct {
	sync.Mutex
	cfg                                journeyclient.Config
	epoch                              uint64
	bound                              bool
	click, input, key, observe, resize js.Func
	observer                           js.Value
	rooms                              map[string]*chattoneRoomState
	previews                           map[string]*chattonePreview
	mounts                             map[string]js.Value
	noticeSeen                         bool
	session                            chattoneSession
}

var chattoneBrowser = &chattoneBrowserController{}

func disposeChattoneBrowser() {
	c := chattoneBrowser
	c.Lock()
	bound := c.bound
	c.bound = false
	c.epoch++
	c.rooms = nil
	c.previews = nil
	c.mounts = nil
	c.Unlock()
	if !bound {
		return
	}
	doc := js.Global().Get("document")
	c.observer.Call("disconnect")
	doc.Call("removeEventListener", "click", c.click)
	doc.Call("removeEventListener", "input", c.input)
	doc.Call("removeEventListener", "keydown", c.key)
	js.Global().Call("removeEventListener", "resize", c.resize)
	c.click.Release()
	c.input.Release()
	c.key.Release()
	c.resize.Release()
	c.observe.Release()
}

func configureChattoneBrowser(cfg journeyclient.Config) {
	c := chattoneBrowser
	c.Lock()
	c.cfg = cfg
	c.epoch++
	c.rooms = make(map[string]*chattoneRoomState)
	c.previews = make(map[string]*chattonePreview)
	c.mounts = make(map[string]js.Value)
	c.noticeSeen = false
	c.session = chattoneSession{}
	bind := !c.bound
	c.bound = true
	c.Unlock()
	if bind {
		doc := js.Global().Get("document")
		c.click = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				chattoneClick(args[0])
			}
			return nil
		})
		c.input = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				chattoneInput(args[0])
			}
			return nil
		})
		c.key = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				chattoneKey(args[0])
			}
			return nil
		})
		doc.Call("addEventListener", "click", c.click)
		doc.Call("addEventListener", "input", c.input)
		doc.Call("addEventListener", "keydown", c.key)
		c.resize = js.FuncOf(func(js.Value, []js.Value) any {
			nodes := doc.Call("querySelectorAll", "[data-chattone=toolbar]")
			for i := 0; i < nodes.Length(); i++ {
				chattonePaint(nodes.Index(i))
			}
			return nil
		})
		js.Global().Call("addEventListener", "resize", c.resize)
		c.observe = js.FuncOf(func(js.Value, []js.Value) any { chattoneFindMounts(); return nil })
		c.observer = js.Global().Get("MutationObserver").New(c.observe)
		c.observer.Call("observe", doc.Get("body"), map[string]any{"childList": true, "subtree": true})
	}
	chattoneFindMounts()
}
func chattoneScope(mount js.Value) string {
	return domAttribute(mount, "data-conversation") + ":" + domAttribute(mount, "data-target") + ":" + domAttribute(mount, "data-scope")
}
func chattoneFindMounts() {
	nodes := js.Global().Get("document").Call("querySelectorAll", "[data-chattone='toolbar']")
	for i := 0; i < nodes.Length(); i++ {
		mount := nodes.Index(i)
		room := domAttribute(mount, "data-conversation")
		scope := chattoneScope(mount)
		if room == "" {
			continue
		}
		c := chattoneBrowser
		c.Lock()
		previous := c.mounts[scope]
		existing := c.rooms[room]
		if previous.Truthy() && previous.Equal(mount) && existing != nil && (existing.loading || time.Now().Before(existing.expires)) {
			c.Unlock()
			chattonePaint(mount)
			continue
		}
		c.mounts[scope] = mount
		if c.previews[scope] == nil {
			c.previews[scope] = &chattonePreview{}
		}
		row := c.rooms[room]
		fetch := c.session.Allow() && (row == nil || (!row.loading && row.errorCode == "" && time.Now().After(row.expires)))
		if fetch {
			row = &chattoneRoomState{loading: true}
			c.rooms[room] = row
		}
		epoch, cfg := c.epoch, c.cfg
		c.Unlock()
		chattonePaint(mount)
		if fetch {
			go func(room string, epoch uint64, cfg journeyclient.Config) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				reply, err := chattoneRequest(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), "suggestion", chattoneDraftRequest{ConversationID: room})
				ui.PostAsync(func() {
					c.Lock()
					if c.epoch != epoch {
						c.Unlock()
						return
					}
					expires := time.Now().Add(time.Hour)
					code := ""
					if err != nil {
						c.session.Record(err)
						code = chattoneErrorCode(err)
						expires = time.Now().Add(time.Minute)
					}
					c.rooms[room] = &chattoneRoomState{reply: reply, errorCode: code, expires: expires}
					mounts := []js.Value{}
					for _, m := range c.mounts {
						if m.Get("isConnected").Truthy() && domAttribute(m, "data-conversation") == room {
							mounts = append(mounts, m)
						}
					}
					c.Unlock()
					for _, m := range mounts {
						chattonePopulate(m, reply)
						chattonePaint(m)
					}
				})
			}(room, epoch, cfg)
		} else if row != nil && !row.loading {
			chattonePopulate(mount, row.reply)
			chattonePaint(mount)
		}
	}
}
func chattonePopulate(mount js.Value, reply chattoneHTTPReply) {
	if !reply.Enabled {
		return
	}
	locale := domAttribute(mount, "data-locale")
	target := domAttribute(mount, "data-target")
	node := chatui.RenderChattoneToolbar(chatui.ChattoneToolbarProps{Locale: locale, Target: target, Conversation: domAttribute(mount, "data-conversation"), Draft: "one two three", Styles: reply.Styles, Suggestion: reply.Suggestion, Enabled: true})
	markup, err := ui.RenderToString(node)
	if err != nil {
		return
	}
	// Only replace the registry-controlled group; the textarea and its selection stay intact.
	doc := js.Global().Get("document")
	scratch := doc.Call("createElement", "div")
	scratch.Set("innerHTML", markup)
	group := scratch.Call("querySelector", ".chattone-toolbar-choices")
	current := mount.Call("querySelector", ".chattone-toolbar-choices")
	if group.Truthy() && current.Truthy() {
		current.Set("innerHTML", group.Get("innerHTML"))
	}
}
func chattonePaint(mount js.Value) {
	c := chattoneBrowser
	c.Lock()
	row := c.rooms[domAttribute(mount, "data-conversation")]
	preview := c.previews[chattoneScope(mount)]
	seen := c.noticeSeen
	available := row != nil && !row.loading && row.errorCode == "" && row.reply.Enabled
	disabled := domAttribute(mount, "data-disabled") == "true"
	if row != nil && !row.loading && row.errorCode == "" {
		mount.Call("setAttribute", "data-enabled", boolText(row.reply.Enabled))
	}
	busy := preview != nil && preview.Busy
	c.Unlock()
	field := js.Global().Get("document").Call("getElementById", domAttribute(mount, "data-target"))
	ready := field.Truthy() && len(strings.Fields(field.Get("value").String())) >= 3
	if field.Truthy() && field.Get("value").String() == "" {
		c.Lock()
		if preview != nil && (preview.Original != "" || preview.Busy) {
			preview.Clear()
			busy = false
		}
		c.Unlock()
	}
	mount.Set("hidden", !available)
	mount.Call("setAttribute", "data-available", boolText(available))
	mount.Call("setAttribute", "data-ready", boolText(ready))
	mount.Call("setAttribute", "aria-busy", boolText(busy))
	buttons := mount.Call("querySelectorAll", "[data-chattone-action='rewrite']")
	for i := 0; i < buttons.Length(); i++ {
		buttons.Index(i).Set("disabled", !available || disabled || busy)
	}
	notice := mount.Call("querySelector", ".chattone-notice")
	if notice.Truthy() {
		notice.Set("hidden", seen && !mount.Get("__chattoneFirstNotice").Truthy())
	}
	if busy {
		chattoneStatus(mount, "working")
	}
	chattonePaintPreview(mount)
}
func chattonePaintPreview(mount js.Value) {
	c := chattoneBrowser
	c.Lock()
	p := c.previews[chattoneScope(mount)]
	var props chatui.ChattonePreviewProps
	if p != nil {
		props = chatui.ChattonePreviewProps{Locale: domAttribute(mount, "data-locale"), Original: p.Original, Rewritten: p.Rewritten, ShowChanges: p.ShowChanges}
	}
	c.Unlock()
	markup, err := ui.RenderToString(chatui.RenderChattonePreview(props))
	if err != nil {
		return
	}
	region := mount.Call("querySelector", ".chattone-preview")
	digest := sha256.Sum256([]byte(markup))
	stamp := hex.EncodeToString(digest[:])
	if region.Truthy() && domAttribute(region, "data-chattone-preview-stamp") != stamp {
		region.Call("setAttribute", "data-chattone-preview-stamp", stamp)
		region.Set("innerHTML", markup)
	}
}
func chattoneStatus(mount js.Value, code string) {
	status := mount.Call("querySelector", ".chattone-status")
	if status.Truthy() {
		text := domAttribute(mount, "data-copy-"+code)
		if text == "" && code != "" {
			text = domAttribute(mount, "data-copy-unavailable")
		}
		if status.Get("textContent").String() != text {
			status.Set("textContent", text)
		}
	}
}
func chattoneMountFor(target string) js.Value {
	nodes := js.Global().Get("document").Call("querySelectorAll", "[data-chattone='toolbar']")
	for i := 0; i < nodes.Length(); i++ {
		if domAttribute(nodes.Index(i), "data-target") == target {
			return nodes.Index(i)
		}
	}
	return js.Undefined()
}
func chattoneInput(event js.Value) {
	field := event.Get("target")
	if !field.Truthy() || field.Get("id").Type() != js.TypeString {
		return
	}
	mount := chattoneMountFor(field.Get("id").String())
	if !mount.Truthy() {
		return
	}
	c := chattoneBrowser
	c.Lock()
	if p := c.previews[chattoneScope(mount)]; p != nil {
		p.Edit(field.Get("value").String())
	}
	c.Unlock()
	chattoneFindMounts()
	chattonePaint(mount)
}
func chattoneKey(event js.Value) {
	if event.Get("key").String() == "Escape" {
		if target := event.Get("target"); target.Truthy() && target.Get("closest").Type() == js.TypeFunction {
			if mount := target.Call("closest", "[data-chattone=toolbar]"); mount.Truthy() {
				if button := mount.Call("querySelector", "[data-chattone-action=rewrite]"); button.Truthy() {
					event.Call("preventDefault")
					chatui.CloseWritingStyleComposer(button, true)
				}
			}
		}
		return
	}
	if !event.Get("altKey").Truthy() || !event.Get("shiftKey").Truthy() || event.Get("ctrlKey").Truthy() || event.Get("metaKey").Truthy() || event.Get("isComposing").Truthy() {
		return
	}
	field := event.Get("target")
	if !field.Truthy() || field.Get("id").Type() != js.TypeString {
		return
	}
	mount := chattoneMountFor(field.Get("id").String())
	if !mount.Truthy() {
		return
	}
	key := event.Get("code").String()
	index := ""
	switch key {
	case "Digit1":
		index = "1"
	case "Digit2":
		index = "2"
	case "Digit3":
		index = "3"
	default:
		return
	}
	button := mount.Call("querySelector", "[data-index='"+index+"']")
	if button.Truthy() && !button.Get("disabled").Truthy() {
		event.Call("preventDefault")
		button.Call("click")
	}
}
func chattoneClick(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return
	}
	panels := js.Global().Get("document").Call("querySelectorAll", "[data-chat-layer=writing-style]:not([hidden])")
	for i := 0; i < panels.Length(); i++ {
		mount := panels.Index(i).Call("closest", "[data-chattone=toolbar]")
		if !mount.Call("contains", target).Bool() {
			chatui.CloseWritingStyleComposer(mount.Call("querySelector", "[data-chattone-action=rewrite]"), false)
		}
	}
	button := target.Call("closest", "[data-chattone-action]")
	if !button.Truthy() || button.Get("disabled").Truthy() {
		return
	}
	mount := button.Call("closest", "[data-chattone='toolbar']")
	if !mount.Truthy() {
		return
	}
	event.Call("preventDefault")
	switch domAttribute(button, "data-chattone-action") {
	case "toggle":
		chatui.ToggleWritingStyleComposer(button)
		chattoneFindMounts()
	case "rewrite":
		chatui.OpenWritingStyleComposer(button)
		c := chattoneBrowser
		c.Lock()
		seen := c.noticeSeen
		c.noticeSeen = true
		c.Unlock()
		mount.Set("__chattoneFirstNotice", !seen)
		if notice := mount.Call("querySelector", ".chattone-notice"); notice.Truthy() {
			notice.Set("hidden", seen)
		}
		chattoneRewrite(mount, domAttribute(button, "data-style"))
	case "undo":
		c := chattoneBrowser
		c.Lock()
		p := c.previews[chattoneScope(mount)]
		original, ok := "", false
		if p != nil {
			original, ok = p.Undo()
		}
		c.Unlock()
		if ok {
			chattoneSetDraft(mount, original)
			chattoneStatus(mount, "")
			chattonePaintPreview(mount)
		}
	case "changes":
		c := chattoneBrowser
		c.Lock()
		if p := c.previews[chattoneScope(mount)]; p != nil {
			p.ShowChanges = !p.ShowChanges
		}
		c.Unlock()
		chattonePaintPreview(mount)
		chattoneFocus(mount)
	}
}
func chattoneRewrite(mount js.Value, style string) {
	field := js.Global().Get("document").Call("getElementById", domAttribute(mount, "data-target"))
	if !field.Truthy() || field.Get("disabled").Truthy() {
		return
	}
	typed := field.Get("value").String()
	room := domAttribute(mount, "data-conversation")
	scope := chattoneScope(mount)
	c := chattoneBrowser
	c.Lock()
	row := c.rooms[room]
	p := c.previews[scope]
	if p == nil || row == nil || row.loading || !row.reply.Enabled || row.errorCode != "" {
		c.Unlock()
		return
	}
	serial, ok := p.Begin(typed)
	epoch, cfg := c.epoch, c.cfg
	c.Unlock()
	if !ok {
		return
	}
	chattonePaint(mount)
	chattoneFocus(mount)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		reply, err := chattoneRequest(ctx, http.DefaultClient, personaChatHTTPConfig(cfg), "rewrite", chattoneDraftRequest{room, typed, style})
		ui.PostAsync(func() {
			current := chattoneMountFor(domAttribute(mount, "data-target"))
			if !current.Truthy() || chattoneScope(current) != scope {
				c.Lock()
				if c.epoch == epoch {
					p.Clear()
				}
				c.Unlock()
				return
			}
			field := js.Global().Get("document").Call("getElementById", domAttribute(current, "data-target"))
			if !field.Truthy() {
				return
			}
			c.Lock()
			if c.epoch != epoch {
				c.Unlock()
				return
			}
			output := reply.Draft
			if err != nil {
				output = ""
			}
			c.noticeSeen = true
			accepted := p.Complete(serial, typed, field.Get("value").String(), output)
			c.Unlock()
			code := ""
			if err != nil {
				code = chattoneErrorCode(err)
				if code == "disabled" {
					c.Lock()
					if row := c.rooms[room]; row != nil {
						row.reply.Enabled = false
					}
					c.Unlock()
				}
			} else if !accepted {
				code = "edited"
			}
			if accepted {
				chattoneSetDraft(current, reply.Draft)
			}
			chattonePaint(current)
			chattoneStatus(current, code)
		})
	}()
}
func chattoneSetDraft(mount js.Value, text string) {
	field := js.Global().Get("document").Call("getElementById", domAttribute(mount, "data-target"))
	if !field.Truthy() {
		return
	}
	field.Set("value", text)
	field.Call("dispatchEvent", js.Global().Get("Event").New("input", map[string]any{"bubbles": true}))
	chattoneFocus(mount)
}
func chattoneFocus(mount js.Value) {
	field := js.Global().Get("document").Call("getElementById", domAttribute(mount, "data-target"))
	if field.Truthy() {
		field.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	}
}
