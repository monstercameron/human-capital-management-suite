//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"syscall/js"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Issue 3726: fills the journey page's "Discussed in" slot with the
// conversations the server says this viewer may open, and remembers a journey
// the viewer sends to chat so it appears there. The slot stays hidden when
// there is nothing to show, so a viewer never sees a hint of a conversation
// they cannot open.

var journeyDiscussedInstalled bool

func bindJourneyDiscussed(cfg journeyclient.Config) {
	if journeyDiscussedInstalled {
		return
	}
	document := js.Global().Get("document")
	observer := js.Global().Get("MutationObserver")
	if !document.Truthy() || !observer.Truthy() {
		return
	}
	journeyDiscussedInstalled = true
	watch := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		refreshJourneyDiscussed(cfg)
		return nil
	})
	observer.New(watch).Call("observe", document.Get("body"), map[string]any{"childList": true, "subtree": true})
	refreshJourneyDiscussed(cfg)
}

// refreshJourneyDiscussed fills the slot once per journey; a re-rendered slot
// loses its marker and is filled again.
func refreshJourneyDiscussed(cfg journeyclient.Config) {
	slot := js.Global().Get("document").Call("querySelector", "[data-projectui-discussed]")
	if !slot.Truthy() {
		return
	}
	journeyID := projectCurrentJourney()
	if journeyID == "" || projectAttr(slot, "data-filled-for") == journeyID {
		return
	}
	slot.Call("setAttribute", "data-filled-for", journeyID)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		reply, err := journeyDiscussionsCall(ctx, http.DefaultClient, chatBrowser.config(cfg), journeyID, "")
		if err != nil {
			// Not shown: a refusal and an absence read the same, and a failed
			// read must not claim the journey was never discussed.
			return
		}
		fillJourneyDiscussed(slot, reply)
	}()
}

func fillJourneyDiscussed(slot js.Value, reply journeyDiscussionsReply) {
	if !slot.Get("isConnected").Bool() {
		return
	}
	rows := journeyDiscussedRows(reply.Items, projectAttr(slot, "data-text-direct"), projectAttr(slot, "data-text-open"))
	slot.Set("innerHTML", "")
	if len(rows) == 0 && !reply.Incomplete {
		slot.Set("hidden", true)
		return
	}
	if len(rows) > 0 {
		slot.Call("appendChild", projectElement("h2", "projectui-journey-discussed-title", projectAttr(slot, "data-text-heading")))
		list := projectElement("ul", "projectui-journey-discussed-list", "")
		for _, row := range rows {
			item := projectElement("li", "", "")
			link := projectElement("a", "projectui-journey-discussed-link", row.Label)
			link.Call("setAttribute", "href", row.Href)
			link.Call("setAttribute", "aria-label", row.Aria)
			item.Call("appendChild", link)
			list.Call("appendChild", item)
		}
		slot.Call("appendChild", list)
	}
	if reply.Incomplete {
		slot.Call("appendChild", projectElement("p", "projectui-journey-discussed-note", projectAttr(slot, "data-text-incomplete")))
	}
	slot.Set("hidden", false)
}

// recordJourneyShare tells the server the viewer put this journey in that
// conversation, then reloads the slot. The message is already sent, so a
// failure here changes nothing the person sees.
func recordJourneyShare(cfg journeyclient.Config, href, conversationID string) {
	journeyID := journeyIDFromHref(href)
	if journeyID == "" || conversationID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := journeyDiscussionsCall(ctx, http.DefaultClient, cfg, journeyID, conversationID); err != nil {
		return
	}
	if slot := js.Global().Get("document").Call("querySelector", "[data-projectui-discussed]"); slot.Truthy() {
		slot.Call("removeAttribute", "data-filled-for")
		refreshJourneyDiscussed(cfg)
	}
}
