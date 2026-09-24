//go:build js && wasm

package main

import (
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// The chat page is a component of its own in the browser. The route loader
// still produces the first model, but every later change -- a reaction, a
// picker, a pane drag, a stream event -- repaints from chatBrowser's live
// model by bumping this component's state. Before this, each of those went
// through the router's Revalidate: the loaders ran again, the skeleton
// showed, and the page blocked for about a second per pass, which the reader
// saw as flicker after every action.

type chatPageProps struct {
	View productui.View
}

var chatRerenderEpoch uint64

func init() {
	productui.ChatPageComponent = func(view productui.View) ui.Node {
		return ui.CreateElement(renderChatPage, chatPageProps{View: view})
	}
}

func renderChatPage(props chatPageProps) ui.Node {
	tick := ui.UseState(0)
	ui.UseEffectOf(func() func() {
		chatRerenderEpoch++
		epoch := chatRerenderEpoch
		chatRerender = func() { tick.Update(func(n int) int { return n + 1 }) }
		installChatSoundUnlock()
		stopChatSoundPolling := startChatSoundPolling()
		popstate := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			chatHistory.restore(args[0], chatBrowser.config(journeyclient.Config{}))
			return nil
		})
		js.Global().Call("addEventListener", "popstate", popstate)
		hashChanged := js.FuncOf(func(js.Value, []js.Value) any {
			openChatChannelFragment(chatBrowser.config(journeyclient.Config{}))
			openChatPersonFragment(chatBrowser.config(journeyclient.Config{}))
			return nil
		})
		js.Global().Call("addEventListener", "hashchange", hashChanged)
		unknownClick := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			event := args[0]
			if event.Get("button").Int() != 0 || event.Get("ctrlKey").Bool() || event.Get("metaKey").Bool() || event.Get("shiftKey").Bool() || event.Get("altKey").Bool() || event.Get("defaultPrevented").Bool() {
				return nil
			}
			target := event.Get("target")
			if !target.Truthy() || !target.Get("closest").Truthy() {
				return nil
			}
			anchor := target.Call("closest", "a.chat-channel-reference")
			if !anchor.Truthy() || anchor.Get("dataset").Get("action").Truthy() {
				return nil
			}
			resetChatChannelFragment()
			time.AfterFunc(time.Millisecond, func() {
				if chatRerenderEpoch == epoch {
					openChatChannelFragment(chatBrowser.config(journeyclient.Config{}))
				}
			})
			return nil
		})
		js.Global().Get("document").Call("addEventListener", "click", unknownClick)
		return func() {
			stopChatSoundPolling()
			js.Global().Call("removeEventListener", "popstate", popstate)
			popstate.Release()
			js.Global().Get("document").Call("removeEventListener", "click", unknownClick)
			unknownClick.Release()
			js.Global().Call("removeEventListener", "hashchange", hashChanged)
			hashChanged.Release()
			resetChatChannelFragment()
			chatPersonFragments.claim("")
			chatHistory.reset()
			if chatRerenderEpoch == epoch {
				chatRerenderEpoch++
				chatRerender = nil
				cancelChatSubscription()
				clearChatMediaCache()
				chatBrowser.invalidateChatEmbeds()
			}
		}
	}, struct{}{})
	model := chatBrowser.snapshot()
	model.GiphyAPIKey = chatBrowser.config(journeyclient.Config{}).GiphyAPIKey
	if model.State == "" && len(model.Conversations) == 0 && model.SelectedID == "" {
		// Nothing has been adopted yet: the loader's model is the only one.
		model = props.View.Chat
	}
	// The route's initial history state is available only once the configured
	// model carries its tenant and viewer. Retry on renders until that identity
	// exists; empty identity would make the first chat entry unsafe to restore.
	chatHistory.seed(model)
	ui.UseEffectOf(func() func() {
		if model.State == chatui.StateReady {
			recordSelectedChatVisit(model.CurrentTenantID, model.CurrentUser, model.SelectedID)
		}
		return func() { releaseSelectedChatVisit(model.CurrentTenantID, model.CurrentUser) }
	}, struct {
		Tenant, Principal, Selected string
		Ready                       bool
	}{model.CurrentTenantID, model.CurrentUser, model.SelectedID, model.State == chatui.StateReady})
	ui.UseEffectOf(func() func() { resolveVisibleChatEmbeds(chatBrowser.config(journeyclient.Config{})); return nil }, chatEmbedFingerprint(model))
	ui.UseEffectOf(func() func() { resolveVisibleChatDocs(chatBrowser.config(journeyclient.Config{})); return nil }, chatDocPreviewFingerprint(model))
	return productui.BuildChatPage(props.View, model)
}
