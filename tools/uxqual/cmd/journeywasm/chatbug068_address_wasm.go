//go:build js && wasm

package main

import (
	"sync"
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// CHATBUG-068 and the address bar. Typing another fragment into the address of
// the tab that has Chat open and pressing Enter changes the address without a
// load, so the page has to read it as it changes: every address the router
// knows (a channel or direct conversation, a thread or message link, a person,
// Saved, Moderation, a search) is read here, and Back and Forward arrive the
// same way. The listener is installed once and lives as long as the page; the
// Chat page's own mount and unmount cannot leave an address change unheard.

var chatAddressWatch struct {
	sync.Once
	listener js.Func
}

// installChatAddressWatch starts listening for address changes. It is called
// by every mount of the Chat page and acts once.
func installChatAddressWatch() {
	chatAddressWatch.Do(func() {
		chatAddressWatch.listener = js.FuncOf(func(js.Value, []js.Value) any {
			chatAddressChanged()
			return nil
		})
		js.Global().Call("addEventListener", "hashchange", chatAddressWatch.listener)
	})
}

// chatAddressChanged applies the address the tab now shows. Each reader below
// acts once per address (the claims), and the addresses this page writes itself
// are claimed as it writes them (chatClaimOwnAddress), so what is read here is
// a change made from outside: typing, Back or Forward.
func chatAddressChanged() {
	if currentPath() != productui.Path(productui.PageChat) {
		return
	}
	cfg := chatBrowser.config(journeyclient.Config{})
	chatPageRestore(cfg)
	openChatShareFragment(cfg)
	openChatChannelFragment(cfg)
	openChatPersonFragment(cfg)
}

// chatClaimOwnAddress records the "#channel=" address this tab has just written
// itself (a click on a conversation, a thread, a restored entry) as handled. An
// address the page wrote is never news: only a change made from outside, by
// typing or by Back and Forward, is read as a request to move the open
// conversation. Without this, a list read that landed between a click and the
// write of its address found the previous address unclaimed and took the tab
// back to it.
func chatClaimOwnAddress() {
	location := js.Global().Get("location")
	if !location.Truthy() {
		return
	}
	hash := location.Get("hash").String()
	active := chatBrowser.config(journeyclient.Config{})
	if id, _ := currentChatChannelFragment(); id == "" {
		resetChatChannelFragment()
		return
	}
	chatBrowser.claimChannelFragment(active.Tenant + "\x00" + active.Subject + "\x00" + hash)
}

// chatApplyAddressTab shows Conversation details when the address asked for a
// tab ("#channel=general&tab=docs"). It runs after the conversation the address
// names has been selected, and writes no history entry of its own: the address
// the person typed is the entry.
func chatApplyAddressTab(id, hash string) {
	if _, tab := chatChannelAddress(hash); tab == "" {
		return
	}
	opened := false
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.SelectedID == id && !model.ShowDetails {
			model.ShowDetails, model.RailMenuID, model.SidebarOpen = true, "", false
			opened = true
		}
	})
	if !opened {
		return
	}
	chatHistory.replace(chatBrowser.snapshot())
	refreshChatRoute()
	go loadChatMembers(chatBrowser.config(journeyclient.Config{}))
}
