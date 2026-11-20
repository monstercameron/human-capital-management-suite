package main

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// personaChatOutcome is one receipt of the agent activity feed: how a finished
// agent run of the open conversation ended, for any asker (#3803).
type personaChatOutcome struct {
	ReceiptID      string `json:"receipt_id"`
	PostID         string `json:"post_id"`
	ThreadID       string `json:"thread_id"`
	ConversationID string `json:"conversation_id"`
	InvokerID      string `json:"invoker_id"`
	PersonaID      string `json:"persona_id"`
	AgentName      string `json:"agent_name"`
	Outcome        string `json:"outcome"`
}

// personaChatOutcomes keeps the receipts that belong to the open conversation
// and stand on their own: a receipt without a run, a message or an outcome word,
// or one of another conversation, is dropped rather than drawn. The result is a
// new slice; a render in flight may be reading the old one.
func personaChatOutcomes(received []personaChatOutcome, conversation string) []chatui.PersonaOutcomeReceipt {
	out := make([]chatui.PersonaOutcomeReceipt, 0, len(received))
	seen := make(map[string]bool, len(received))
	for _, outcome := range received {
		if outcome.ConversationID != conversation || strings.TrimSpace(outcome.ReceiptID) == "" || strings.TrimSpace(outcome.PostID) == "" || strings.TrimSpace(outcome.Outcome) == "" || seen[outcome.ReceiptID] {
			continue
		}
		seen[outcome.ReceiptID] = true
		out = append(out, chatui.PersonaOutcomeReceipt{ID: outcome.ReceiptID, PostID: outcome.PostID, ThreadID: outcome.ThreadID, InvokerID: outcome.InvokerID, PersonaID: outcome.PersonaID, AgentName: outcome.AgentName, Outcome: outcome.Outcome})
	}
	return out
}
