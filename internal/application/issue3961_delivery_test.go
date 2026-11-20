package application

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3961: the answer a person reads carries the receipt for the files on
// the thread, public or private, and the private reason marker stays last.
func TestIssue3961_DeliveredAnswerCarriesTheAttachmentReceipt_Integration(t *testing.T) {
	receipt := chat.AttachmentReceipt{Reference: 2}
	t.Run("public answer", func(t *testing.T) {
		room := newAgentUX070Room(t, agentUX070Options{question: "@Policy Helper what changed in the attached checklist?"})
		delivered, err := room.deliver(agentUX070Delivery{attachments: receipt})
		answer := room.assertPublic(delivered, err)
		_, got, found := chat.SplitAttachmentReceipt(answer.Body)
		if !found || got != receipt {
			t.Fatalf("the public answer does not say the files were not read: %q", answer.Body)
		}
	})
	t.Run("private answer", func(t *testing.T) {
		room := newAgentUX070Room(t, agentUX070Options{question: "@Policy Helper what changed in the attached checklist?"})
		delivered, err := room.deliver(agentUX070Delivery{alwaysPrivate: true, attachments: receipt})
		card := room.assertPrivate(delivered, err, chat.PrivateReasonAgent)
		cleaned, _ := chat.SplitPrivateReason(card.Body)
		if _, got, found := chat.SplitAttachmentReceipt(cleaned); !found || got != receipt {
			t.Fatalf("the private card does not say the files were not read: %q", card.Body)
		}
	})
	t.Run("no files means no comment", func(t *testing.T) {
		room := newAgentUX070Room(t, agentUX070Options{question: "@Policy Helper how many PTO hours carry over?"})
		delivered, err := room.deliver(agentUX070Delivery{})
		answer := room.assertPublic(delivered, err)
		if strings.Contains(answer.Body, "chat.agent.attachments") {
			t.Fatalf("an answer with no files carries a receipt: %q", answer.Body)
		}
	})
}
