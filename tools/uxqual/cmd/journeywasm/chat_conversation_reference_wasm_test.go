//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestChatConversationReferenceRichAndPlainClipboard(t *testing.T) {
	plain, rich, ok := chatConversationReferenceFormats("https://hcm.example", "room with/slash", `#Q4_"hiring"`)
	if !ok || plain != `#Q4_"hiring" https://hcm.example/workspace/app/chat#channel=room+with%2Fslash` || !strings.Contains(rich, `href="https://hcm.example/workspace/app/chat#channel=room+with%2Fslash"`) || !strings.Contains(rich, `#Q4_&#34;hiring&#34;`) {
		t.Fatalf("formats plain=%q rich=%q ok=%v", plain, rich, ok)
	}
	if _, _, ok := chatConversationReferenceFormats("javascript:alert(1)", "room", "#room"); ok {
		t.Fatal("unsafe origin produced a clipboard link")
	}
	global, object := js.Global(), js.Global().Get("Object")
	replace := func(name string, value any) {
		old := object.Call("getOwnPropertyDescriptor", global, name)
		t.Cleanup(func() {
			if old.Truthy() {
				object.Call("defineProperty", global, name, old)
			} else {
				global.Get("Reflect").Call("deleteProperty", global, name)
			}
		})
		object.Call("defineProperty", global, name, map[string]any{"value": value, "configurable": true})
	}
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "reader", Bearer: "token", Locale: "en-US"}
	chatBrowser.reset(nil, cfg, nil)
	defer chatBrowser.reset(nil, journeyclient.Config{}, nil)
	chatBrowser.mutate(func(m *chatui.Model) {
		m.Conversations = []chatui.Conversation{{ID: "room", Name: "Q4 hiring huddle"}, {ID: "dm", Name: "Evelyn Morgan", Kind: chatui.DirectMessage}}
		m.SelectedID = "dm"
	})
	replace("location", js.ValueOf(map[string]any{"origin": "https://hcm.example"}))
	blob := js.FuncOf(func(this js.Value, args []js.Value) any {
		this.Set("body", args[0].Index(0).String())
		this.Set("mime", args[1].Get("type"))
		return nil
	})
	defer blob.Release()
	item := js.FuncOf(func(this js.Value, args []js.Value) any { this.Set("parts", args[0]); return nil })
	defer item.Release()
	replace("Blob", blob)
	replace("ClipboardItem", item)
	thenable := object.New()
	resolve := js.FuncOf(func(_ js.Value, args []js.Value) any { args[0].Invoke(); return nil })
	defer resolve.Release()
	thenable.Set("then", resolve)
	var written js.Value
	write := js.FuncOf(func(_ js.Value, args []js.Value) any {
		written = args[0].Index(0)
		return thenable
	})
	defer write.Release()
	textWrites := []string{}
	writeText := js.FuncOf(func(_ js.Value, args []js.Value) any {
		textWrites = append(textWrites, args[0].String())
		return thenable
	})
	defer writeText.Release()
	clipboard := object.New()
	clipboard.Set("write", write)
	clipboard.Set("writeText", writeText)
	navigator := object.New()
	navigator.Set("clipboard", clipboard)
	replace("navigator", navigator)
	copyChatConversationReference("room", "#Q4_hiring_huddle")
	if !written.Truthy() || len(textWrites) != 0 {
		t.Fatalf("rich clipboard unavailable, fallback writes=%#v", textWrites)
	}
	parts := written.Get("parts")
	if got := parts.Get("text/plain").Get("body").String(); got != "#Q4_hiring_huddle https://hcm.example/workspace/app/chat#channel=room" {
		t.Fatalf("plain part = %q", got)
	}
	if got := parts.Get("text/html").Get("body").String(); got != `<a href="https://hcm.example/workspace/app/chat#channel=room">#Q4_hiring_huddle</a>` {
		t.Fatalf("html part = %q", got)
	}
	if chatBrowser.currentNotice() != "Conversation link copied" {
		t.Fatalf("success notice = %q", chatBrowser.currentNotice())
	}
	copyChatConversationReference("unknown", "#unknown")
	if len(textWrites) != 0 {
		t.Fatal("unknown conversation reached clipboard")
	}
	object.Call("defineProperty", global, "ClipboardItem", map[string]any{"value": js.Undefined(), "configurable": true})
	copyChatConversationReference("dm", "#Evelyn_Morgan")
	if len(textWrites) != 1 || textWrites[0] != "#Evelyn_Morgan https://hcm.example/workspace/app/chat#channel=dm" {
		t.Fatalf("plain fallback = %#v", textWrites)
	}
}
