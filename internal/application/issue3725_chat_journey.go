package application

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// chatJourneyDirectory is the chat service's view of the journey owner (issue
// 3725). It answers one question for a typed journey reference: may this person
// open this workflow request now, and what kind of workflow is it. The read goes
// through the intent service as the caller, so the owner's own read rule decides;
// the chat caller's claims are never an input.
type chatJourneyDirectory struct{ Intents ChatWorkIntentReader }

// JourneyVisible implements chat.JourneyDirectory.
func (d chatJourneyDirectory) JourneyVisible(ctx context.Context, p chat.Principal, intentID string) (string, error) {
	trusted, ok := trust.FromContext(ctx)
	if !ok || trusted == nil || trusted.Tenant().String() != p.TenantID || trusted.Subject() != p.SubjectID {
		return "", chat.ErrPermissionDenied
	}
	if d.Intents == nil {
		return "", chat.ErrUnavailable
	}
	inst, err := d.Intents.ReadIntent(ctx, trusted, intentID)
	switch {
	case err == nil && inst != nil:
		return chatWorkHumanize(inst.GetDefinition().GetIntentTypeId()), nil
	case err == nil, errors.Is(err, ErrChatWorkNotFound), errors.Is(err, ErrChatWorkDenied):
		// A missing journey and a refused read look the same to the caller, so a
		// reference never confirms that a journey exists.
		return "", chat.ErrPermissionDenied
	default:
		return "", chat.ErrUnavailable
	}
}

// chatJourneyShareLinker records, in the chat store, that a journey was shared
// into a conversation. The row holds ids only.
type chatJourneyShareLinker struct{ Links ChatWorkLinker }

// LinkJourneyShare implements chat.JourneyShareLinker.
func (l chatJourneyShareLinker) LinkJourneyShare(ctx context.Context, tenant, conversation, intentID, sharedBy string) error {
	if l.Links == nil {
		return chat.ErrUnavailable
	}
	return l.Links.LinkWork(ctx, chatstore.WorkLink{TenantID: tenant, ConversationID: conversation, Kind: chatstore.WorkLinkJourneyShare, SourceID: intentID, ProposedByID: sharedBy})
}
