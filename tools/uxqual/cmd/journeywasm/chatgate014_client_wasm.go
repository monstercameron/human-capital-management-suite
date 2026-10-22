//go:build js && wasm

package main

import (
	"syscall/js"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// chatgateAsk reports whether the page should ask about a channel's gate at all:
// the feature is on, and the list of gates, when it was read, holds the channel.
// A read that fails keeps what the page had: these are conveniences and never
// blank the panel or the composer.
func chatgateAsk(cfg journeyclient.Config, room string) (journeyclient.Config, bool) {
	active := chatBrowser.config(cfg)
	if active.Bearer == "" || room == "" {
		return active, false
	}
	model := chatBrowser.snapshot()
	if features := model.ChatFeatures; features != nil && !features.Gates {
		return active, false
	}
	// The list of gates was read and this channel is not in it: nothing to ask.
	return active, !chatgateKnownUngated(model.GateJoins, room)
}

// chatgateRefreshLabels reads the labels of a conversation and keeps them on the
// model for Conversation details.
func chatgateRefreshLabels(cfg journeyclient.Config, room string) {
	active, ask := chatgateAsk(cfg, room)
	if !ask {
		return
	}
	reply, status := chatgateFetch(active, chatgateClientRequest{Action: chatgateLabelsAction, Conversation: room})
	if status != 200 {
		return
	}
	labels, ok := chatgateLabels(reply.Result)
	if !ok {
		return
	}
	current := chatBrowser.config(cfg)
	if current.Tenant != active.Tenant || current.Subject != active.Subject {
		return
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		next := make(map[string][]chatui.GateLabel, len(m.GateLabels)+1)
		for id, held := range m.GateLabels {
			next[id] = held
		}
		next[room] = labels
		m.GateLabels = next
	})
	chatStreamRender.Schedule()
}

// chatgateRefreshAudience reads what the writer may address in a conversation and
// keeps it on the model for the composer.
func chatgateRefreshAudience(cfg journeyclient.Config, room string) {
	active, ask := chatgateAsk(cfg, room)
	if !ask {
		return
	}
	reply, status := chatgateFetch(active, chatgateClientRequest{Action: chatgateAudienceAction, Conversation: room})
	if status != 200 {
		return
	}
	options, ok := chatgateAudienceOptions(reply.Result)
	if !ok {
		return
	}
	current := chatBrowser.config(cfg)
	if current.Tenant != active.Tenant || current.Subject != active.Subject {
		return
	}
	chatBrowser.mutate(func(m *chatui.Model) {
		next := make(map[string]chatui.GateAudienceOptions, len(m.GateAudience)+1)
		for id, held := range m.GateAudience {
			next[id] = held
		}
		next[room] = options
		m.GateAudience = next
	})
	chatStreamRender.Schedule()
}

// chatgateComposerAudience is the audience chosen under the composer's "Visible
// to": no segments for Everyone, or when there is no such control.
func chatgateComposerAudience() ([]string, bool) {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return nil, false
	}
	field := doc.Call("getElementById", chatui.GateAudienceField)
	if !field.Truthy() {
		return nil, false
	}
	return chatui.ParseGateAudience(field.Get("value").String())
}

// chatgateComposerAudienceReset puts the control back to Everyone once a message
// has gone, so no message is addressed by a choice left over from the one before.
func chatgateComposerAudienceReset() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return
	}
	if field := doc.Call("getElementById", chatui.GateAudienceField); field.Truthy() {
		field.Set("value", "")
	}
}

// chatgateAudienceRefused is asked by the sends that cannot carry an audience (an
// agent mention, files): when a group is chosen the message is not sent, and the
// writer is told, because sending it to everyone would be the one wrong outcome.
func chatgateAudienceRefused() bool {
	segments, _ := chatgateComposerAudience()
	if len(segments) == 0 {
		return false
	}
	noteChatAction(chatui.GateAudienceRefusedText(chatBrowser.snapshot().Locale))
	return true
}
