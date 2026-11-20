//go:build js && wasm

package main

import (
	"sync"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// Issue 3603: the shell's Chat link changes the address through the router,
// which fires neither hashchange nor popstate, so a refused "#channel=" link
// stayed on screen as "Conversation unavailable" until a reload. This listener
// outlives the page's mounts (like installChatAddressWatch) and re-reads the
// address after such a link is followed.
var chatShellLinkWatch struct {
	sync.Once
	listener js.Func
}

func installChatShellLinkWatch() {
	chatShellLinkWatch.Do(func() {
		chatShellLinkWatch.listener = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			event := args[0]
			if event.Get("button").Int() != 0 || event.Get("ctrlKey").Bool() || event.Get("metaKey").Bool() || event.Get("shiftKey").Bool() || event.Get("altKey").Bool() {
				return nil
			}
			target := event.Get("target")
			if !target.Truthy() || !target.Get("closest").Truthy() {
				return nil
			}
			anchor := target.Call("closest", "a[href]")
			if !anchor.Truthy() || !chatShellLinkClearsChannel(anchor.Get("href").String(), productui.Path(productui.PageChat)) {
				return nil
			}
			// The router writes the new address after this click returns.
			time.AfterFunc(50*time.Millisecond, func() { ui.PostAsync(chatAddressChanged) })
			return nil
		})
		js.Global().Get("document").Call("addEventListener", "click", chatShellLinkWatch.listener)
	})
}
