//go:build js && wasm

package main

import (
	"context"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Decide from the conversation (#3724). The decision state of every card
// lives in the chat model; the journey cache holds the card's facts. A
// decision is sent as the viewer, with the revision the card was read at, and
// the answer (or a refusal that the proposal changed) refreshes the card.

func setChatJourneyDecision(intentID string, change func(*chatui.JourneyDecisionState)) {
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.JourneyDecisions == nil {
			model.JourneyDecisions = map[string]chatui.JourneyDecisionState{}
		}
		state := model.JourneyDecisions[intentID]
		change(&state)
		model.JourneyDecisions[intentID] = state
	})
	refreshChatRoute()
}

// setChatJourneyDecisionDraft keeps the typed reason without redrawing the
// page on every keystroke, so a failed decision never loses it.
func setChatJourneyDecisionDraft(intentID, text string) {
	chatBrowser.mutate(func(model *chatui.Model) {
		if model.JourneyDecisions == nil {
			model.JourneyDecisions = map[string]chatui.JourneyDecisionState{}
		}
		state := model.JourneyDecisions[intentID]
		state.Reason = text
		model.JourneyDecisions[intentID] = state
	})
}

// decideChatJourney sends the routed approver's decision. It acts only on a
// card the resolver named the viewer as the approver of, with the revision
// that card was read at, and never while another decision is in flight.
func decideChatJourney(cfg journeyclient.Config, intentID string, approve bool) {
	model := chatBrowser.snapshot()
	preview, ok := model.JourneyPreviews[intentID]
	state := model.JourneyDecisions[intentID]
	revision := strings.TrimSpace(preview.Revision)
	if !ok || !preview.WaitsOnViewer || revision == "" || state.Busy != "" {
		return
	}
	reason := strings.TrimSpace(state.Reason)
	if !approve && reason == "" {
		setChatJourneyDecision(intentID, func(s *chatui.JourneyDecisionState) { s.Error = "reason" })
		return
	}
	active := chatBrowser.config(cfg)
	client := chatWorkers
	if active.Tenant == "" || active.Subject == "" || client == nil {
		setChatJourneyDecision(intentID, func(s *chatui.JourneyDecisionState) { s.Error = "unavailable" })
		return
	}
	identity := active.Tenant + "\x00" + active.Subject
	busy := "reject"
	if approve {
		busy = "approve"
	}
	setChatJourneyDecision(intentID, func(s *chatui.JourneyDecisionState) { s.Busy, s.Error = busy, "" })
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		response, err := client.DecideJourney(chatRPCContext(ctx, active), journeyDecisionRequest(intentID, approve, reason, revision))
		var next chatui.JourneyPreview
		if err == nil {
			next = chatJourneyPreviewFromDetail(ctx, active, intentID, response.GetDetail())
		}
		ui.PostAsync(func() {
			chatJourneys.Lock()
			same := chatJourneys.identity == identity
			if same && err == nil {
				chatJourneys.entries[intentID] = chatJourneyEntry{value: next, at: time.Now()}
			} else if same && journeyDecisionFailure(err) == "stale" {
				// The proposal moved on: read the card again so it shows the
				// current proposal and its new revision.
				delete(chatJourneys.entries, intentID)
			}
			chatJourneys.Unlock()
			if !same {
				return
			}
			setChatJourneyDecision(intentID, func(s *chatui.JourneyDecisionState) {
				s.Busy = ""
				if err != nil {
					s.Error = journeyDecisionFailure(err)
					return
				}
				// The reason is spent; the receipt stays beside the card.
				s.Error, s.Reason, s.Receipt, s.Open = "", "", journeyDecisionReceiptFor(approve), true
			})
			chatBrowser.mutate(func(model *chatui.Model) {
				model.JourneyPreviews = chatJourneyProjection(identity, chatJourneyRefs(*model))
			})
			if err != nil && journeyDecisionFailure(err) == "stale" {
				resolveVisibleChatJourneys(cfg)
			}
			refreshChatRoute()
		})
	}()
}
