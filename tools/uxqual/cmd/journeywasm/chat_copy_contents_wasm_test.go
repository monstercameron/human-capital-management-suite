//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

func TestChatCopyContentsClipboardAndThreadLookup(t *testing.T) {
	global := js.Global()
	object := global.Get("Object")
	oldNavigator := object.Call("getOwnPropertyDescriptor", global, "navigator")
	defer func() {
		if oldNavigator.Truthy() {
			object.Call("defineProperty", global, "navigator", oldNavigator)
		} else {
			global.Get("Reflect").Call("deleteProperty", global, "navigator")
		}
	}()
	cfg := journeyclient.Config{Tenant: "tenant", Subject: "reader", Bearer: "token", Locale: "de-DE"}
	chatBrowser.reset(nil, cfg, nil)
	defer chatBrowser.reset(nil, journeyclient.Config{}, nil)
	root := chatui.Message{ID: "root", Body: "first\nsecond\n"}
	reply := chatui.Message{ID: "reply", Body: "reply\nline"}
	chatBrowser.mutate(func(m *chatui.Model) {
		m.SelectedID = "room"
		m.Messages = []chatui.Message{{ID: "other", Body: "other"}}
		m.ShowThread, m.ThreadParentID, m.ThreadParent = true, root.ID, &root
		m.ThreadMessages = []chatui.Message{reply}
	})
	thenable := object.New()
	then := js.FuncOf(func(_ js.Value, args []js.Value) any { args[0].Invoke(); return nil })
	defer then.Release()
	thenable.Set("then", then)
	copied := []string{}
	write := js.FuncOf(func(_ js.Value, args []js.Value) any {
		copied = append(copied, args[0].String())
		return thenable
	})
	defer write.Release()
	clipboard := object.New()
	clipboard.Set("writeText", write)
	navigator := object.New()
	navigator.Set("clipboard", clipboard)
	object.Call("defineProperty", global, "navigator", map[string]any{"value": navigator, "configurable": true})
	if body, ok := chatCopyableBody(chatBrowser.snapshot(), "root"); !ok || body != root.Body {
		t.Fatalf("root lookup=%q ok=%v model=%+v", body, ok, chatBrowser.snapshot())
	}
	if got := chatBrowser.config(journeyclient.Config{}); got.Tenant != cfg.Tenant || got.Bearer != cfg.Bearer {
		t.Fatalf("active config=%+v", got)
	}
	copyChatMessageContents("root")
	copyChatMessageContents("reply")
	if len(copied) != 2 || copied[0] != root.Body || copied[1] != reply.Body {
		t.Fatalf("clipboard bodies = %#v, notice=%q", copied, chatBrowser.currentNotice())
	}
	if got, want := chatBrowser.currentNotice(), productui.ResolveProductLocale(cfg.Locale).Text(chatui.KeyCopyContentsSuccess); got != want {
		t.Fatalf("success notice = %q, want %q", got, want)
	}
	if got := chatBrowser.currentNotice(); got != "Nachrichtentext kopiert" {
		t.Fatalf("success notice was not localized: %q", got)
	}
	copyChatMessageContents("deleted")
	if len(copied) != 2 {
		t.Fatal("unknown/deleted post reached clipboard")
	}
	chatBrowser.mutate(func(m *chatui.Model) { m.ShowThread = false })
	copyChatMessageContents("reply")
	if len(copied) != 2 {
		t.Fatal("closed thread reply reached clipboard")
	}
	rejected := object.New()
	reject := js.FuncOf(func(_ js.Value, args []js.Value) any { args[1].Invoke(); return nil })
	defer reject.Release()
	rejected.Set("then", reject)
	writeRejected := js.FuncOf(func(js.Value, []js.Value) any { return rejected })
	defer writeRejected.Release()
	clipboard.Set("writeText", writeRejected)
	copyChatMessageContents("other")
	if got, want := chatBrowser.currentNotice(), productui.ResolveProductLocale(cfg.Locale).Text(chatui.KeyCopyContentsFailure); got != want {
		t.Fatalf("rejected clipboard notice = %q, want %q", got, want)
	}
	object.Call("defineProperty", global, "navigator", map[string]any{"value": object.New(), "configurable": true})
	copyChatMessageContents("other")
	if got, want := chatBrowser.currentNotice(), productui.ResolveProductLocale(cfg.Locale).Text(chatui.KeyCopyContentsFailure); got != want {
		t.Fatalf("failure notice = %q, want %q", got, want)
	}
}
