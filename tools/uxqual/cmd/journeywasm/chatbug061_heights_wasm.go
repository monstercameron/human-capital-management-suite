//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatbug061RememberCardHeights reads the height of every private answer card on
// the page and keeps it with the model, by the message the card sits under. A
// placeholder for that answer then keeps exactly that room the next time the
// conversation is opened on this page (CHATBUG-061).
func chatbug061RememberCardHeights() {
	document := js.Global().Get("document")
	if !document.Truthy() {
		return
	}
	cards := document.Call("querySelectorAll", ".chat-ephemeral.agent-reply-row[data-agent-reply-state='answered-private'][data-ephemeral-thread]")
	measured := make(map[string]int, cards.Length())
	for index := 0; index < cards.Length(); index++ {
		card := cards.Index(index)
		measured[domAttribute(card, "data-ephemeral-thread")] = card.Get("offsetHeight").Int()
	}
	if len(measured) == 0 {
		return
	}
	// This runs once a second while answer cards are on the page; the model is
	// touched only when a height it does not hold was measured.
	if _, changed := chatbug061MergeHeights(chatBrowser.snapshot().AgentCardHeights, measured); !changed {
		return
	}
	chatBrowser.mutate(func(model *chatui.Model) {
		if next, changed := chatbug061MergeHeights(model.AgentCardHeights, measured); changed {
			model.AgentCardHeights = next
		}
	})
}
