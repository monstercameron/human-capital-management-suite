//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// CHATBUG-052. The address and the tab title follow the Moderation page, the
// search results and the Saved panel. chatPageSync reads what is on screen and
// makes the address say it; chatPageRestore does the reverse for a reload, Back,
// Forward or a pasted link. Neither depends on a timer or on window focus.

// chatPageRestoreEvent asks the Moderation page and the Saved panel to show or
// hide to match the address; its detail is the page kind the address names.
const chatPageRestoreEvent = "chat-page-restore"

var chatPageRoute struct {
	// applied is the fragment the address carries for a page, or "".
	applied string
	// restoring is true while the screen is being moved to match the address,
	// so the changes it makes are not written back as new history entries.
	restoring bool
}

// chatPageOnScreen reads the page that is showing.
func chatPageOnScreen() (kind, query string) {
	document := js.Global().Get("document")
	moderation := document.Call("querySelector", `[data-chatremove-overlay="page"]`).Truthy()
	saved := false
	if panel := document.Call("getElementById", "chatsave-list"); panel.Truthy() {
		saved = !panel.Get("hidden").Bool()
	}
	model := chatBrowser.snapshot()
	return chatPageShowing(moderation, saved, model.Search, model.SearchOpened)
}

// chatPageBaseHref is the address of the conversation alone.
func chatPageBaseHref(conversationID string) string {
	if conversationID != "" {
		return chatHistoryHref(conversationID)
	}
	href := currentPath()
	if query := currentQuery(); query != "" {
		href += "?" + query
	}
	return href
}

// chatPageHref is the address a history entry for conversationID is written
// under: the page's fragment while one is showing, else the conversation's.
func chatPageHref(conversationID string) string {
	if chatPageRoute.applied != "" {
		href := currentPath()
		if query := currentQuery(); query != "" {
			href += "?" + query
		}
		return href + chatPageRoute.applied
	}
	return chatHistoryHref(conversationID)
}

// chatPageSync makes the address and the tab title say what is on screen. A
// page opening from the conversation is a new history entry; one page giving
// way to another, a search being typed, or the page closing rewrites the entry.
func chatPageSync() {
	defer syncChatTabTitle()
	if chatPageRoute.restoring {
		return
	}
	kind, query := chatPageOnScreen()
	fragment := chatPageFragment(kind, query)
	if fragment == chatPageRoute.applied {
		return
	}
	history, ok := browserHistory()
	if !ok {
		return
	}
	current, ok := browserHistoryState(history)
	if !ok {
		return
	}
	clone, ok := cloneBrowserHistoryState(current)
	if !ok {
		return
	}
	previous := chatPageRoute.applied
	chatPageRoute.applied = fragment
	// The address written here is this page's own, so it is claimed as handled
	// (chatClaimOwnAddress) and the address readers never take it for news.
	defer chatClaimOwnAddress()
	if fragment == "" {
		browserHistoryReplaceState(history, clone, chatPageBaseHref(chatBrowser.snapshot().SelectedID))
		return
	}
	href := currentPath()
	if query := currentQuery(); query != "" {
		href += "?" + query
	}
	href += fragment
	if previous != "" {
		browserHistoryReplaceState(history, clone, href)
		return
	}
	if _, pushed := browserCall(history, "pushState", clone, "", href); pushed {
		productHistory.RecordSameRoutePush()
	}
}

// chatPageRestore moves the screen to the page the address names.
func chatPageRestore(cfg journeyclient.Config) {
	location := js.Global().Get("location")
	if !location.Truthy() || chatPageRoute.restoring {
		return
	}
	kind, query := parseChatPageFragment(location.Get("hash").String())
	chatPageRoute.restoring = true
	defer func() {
		chatPageRoute.restoring = false
		chatPageRoute.applied = chatPageFragment(kind, query)
		syncChatTabTitle()
	}()
	js.Global().Get("document").Call("dispatchEvent", js.Global().Get("CustomEvent").New(chatPageRestoreEvent, map[string]any{"detail": kind}))
	model := chatBrowser.snapshot()
	switch {
	case kind == chatui.ChatPageSearch:
		switch {
		case strings.TrimSpace(model.Search) == query && model.SearchOpened:
			chatsearchReturn()
		case strings.TrimSpace(model.Search) != query && model.Callbacks.Search != nil:
			model.Callbacks.Search(query)
		}
	case strings.TrimSpace(model.Search) != "" && !model.SearchOpened && model.Callbacks.Search != nil:
		// The entry the address names has no search on it.
		model.Callbacks.Search("")
	}
}

// installChatPageRoute listens for Back, Forward and edits of the address and
// opens the page a reloaded or pasted address names once the chat is ready.
// The returned function removes the listeners.
func installChatPageRoute(cfg journeyclient.Config) func() {
	restore := js.FuncOf(func(js.Value, []js.Value) any {
		chatPageRestore(chatBrowser.config(cfg))
		return nil
	})
	// A typed address (hashchange) is read by the page-long listener
	// (installChatAddressWatch); this one hears Back and Forward.
	js.Global().Call("addEventListener", "popstate", restore)
	stopped := false
	if kind, _ := parseChatPageFragment(js.Global().Get("location").Get("hash").String()); kind != "" {
		go func() {
			for deadline := time.Now().Add(30 * time.Second); !stopped && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
				if chatBrowser.snapshot().State == chatui.StateReady && js.Global().Get("document").Call("querySelector", ".chat-workspace").Truthy() {
					ui.PostAsync(func() {
						if !stopped {
							chatPageRestore(chatBrowser.config(cfg))
						}
					})
					return
				}
			}
		}()
	}
	return func() {
		stopped = true
		js.Global().Call("removeEventListener", "popstate", restore)
		restore.Release()
	}
}

// syncChatTabTitle puts what the page shows in front of the page's own title.
// The page's own title is whatever the document held before this code first
// changed it, and again each time the shell sets another one.
func syncChatTabTitle() {
	document := js.Global().Get("document")
	if !document.Truthy() {
		return
	}
	root := document.Get("documentElement")
	read := func(name string) string {
		if value := root.Call("getAttribute", name); value.Type() == js.TypeString {
			return value.String()
		}
		return ""
	}
	current := document.Get("title").String()
	base := chatTabTitleBase(current, read("data-hcm-chat-title-last"), read("data-hcm-chat-title-base"))
	kind, query := chatPageOnScreen()
	next := chatTabTitleText(base, chatui.ChatTabTitle(chatBrowser.snapshot(), kind, query))
	if current != next {
		document.Set("title", next)
	}
	root.Call("setAttribute", "data-hcm-chat-title-base", base)
	root.Call("setAttribute", "data-hcm-chat-title-last", next)
}
