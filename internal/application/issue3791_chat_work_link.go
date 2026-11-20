package application

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatapps"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// ChatWorkLinker remembers that a proposal came from a conversation (issue 3791).
type ChatWorkLinker interface {
	LinkWork(context.Context, chatstore.WorkLink) error
}

// linkProposal ties a proposal an installed agent put forward to the
// conversation it came from, so the conversation's work panel can find it. It
// stores only the pointer: the proposal's status is read from its owner, under
// the reader's own authority, whenever the panel is opened. Linking is
// idempotent, so a retried proposal (the intent service returns the same intent
// for the same key) links once. A failure is returned: the person retries, the
// same intent comes back, and the link is made then; a proposal the panel can
// never show is not left behind silently.
func linkProposal(ctx context.Context, links ChatWorkLinker, p chatapps.Proposal, receipt chatapps.ProposalReceipt) error {
	if links == nil || receipt.IntentID == "" {
		return nil
	}
	if err := links.LinkWork(ctx, chatstore.WorkLink{
		TenantID: p.Tenant, ConversationID: p.Conversation, Kind: chatstore.WorkLinkIntentProposal, SourceID: receipt.IntentID,
		ProposedByID: p.Principal, AgentInstallationID: p.AgentInstallation, IntentType: p.IntentType,
	}); err != nil {
		return fmt.Errorf("application: link chat proposal to its conversation: %w", err)
	}
	return nil
}
