package application

import (
	"context"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
)

// AgentAnnouncementDocumentAuthority delegates every document decision to the
// Documents hub. Placement must be the current official placement for this
// exact conversation and version; direct access is checked per audience member.
type AgentAnnouncementDocumentAuthority interface {
	OfficialConversationPlacement(context.Context, string, string, string, string, string, string) (bool, error)
	AuthorizeDocumentRead(context.Context, string, string, string, string, string, string, string) error
}

type AgentAnnouncementAudienceAuthority struct {
	Chat      chat.ConversationService
	Audience  chatrecipient.AudienceFloorAuthority
	Documents AgentAnnouncementDocumentAuthority
	Principal func(context.Context, string) (chat.Principal, error)
}

func (a AgentAnnouncementAudienceAuthority) AuthorizeAnnouncementDocuments(ctx context.Context, tenant, conversationID string, documents []AgentAnnouncementResolvedDocument, citations []string) (bool, int, error) {
	if ctx == nil || a.Chat == nil || a.Audience == nil || a.Documents == nil || a.Principal == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(conversationID) == "" || len(documents) == 0 || len(citations) == 0 {
		return false, len(documents), ErrAgentAnnouncementUnavailable
	}
	principal, err := a.Principal(ctx, tenant)
	if err != nil || principal.TenantID != tenant || strings.TrimSpace(principal.SubjectID) == "" {
		return false, len(documents), ErrAgentAnnouncementDenied
	}
	conversation, err := a.Chat.GetConversation(ctx, chat.GetConversationRequest{Principal: principal, TenantID: tenant, ConversationID: conversationID})
	if err != nil || conversation.TenantID != tenant || conversation.ID != conversationID || !agentAnnouncementChannel(conversation.Kind) || conversation.Archived {
		return false, len(documents), ErrAgentAnnouncementDenied
	}
	snapshot, err := a.Audience.CurrentAudience(ctx, conversation)
	if err != nil {
		return false, len(documents), err
	}
	return authorizeAnnouncementAudience(ctx, tenant, conversationID, a.Documents, snapshot, documents, citations)
}

func authorizeAnnouncementAudience(ctx context.Context, tenant, conversationID string, authority AgentAnnouncementDocumentAuthority, snapshot chatrecipient.AudienceSnapshot, documents []AgentAnnouncementResolvedDocument, citations []string) (bool, int, error) {
	if ctx == nil || authority == nil || snapshot.TenantID != tenant || snapshot.ConversationID != conversationID || snapshot.Revision == 0 || !snapshot.Complete || !snapshot.GuestAndExternalComplete || len(snapshot.CurrentMembers) == 0 || len(documents) == 0 || len(citations) == 0 {
		return false, len(documents), ErrAgentAnnouncementDenied
	}
	for _, member := range snapshot.CurrentMembers {
		if strings.TrimSpace(member.TenantID) == "" || strings.TrimSpace(member.SubjectID) == "" {
			return false, len(documents), ErrAgentAnnouncementDenied
		}
	}
	byID := make(map[string]AgentAnnouncementResolvedDocument, len(documents))
	for _, document := range documents {
		if strings.TrimSpace(document.DocumentID) == "" || strings.TrimSpace(document.Version) == "" || strings.TrimSpace(document.Digest) == "" || document.DocumentID != strings.TrimSpace(document.DocumentID) {
			return false, len(documents), ErrAgentAnnouncementInvalid
		}
		if _, duplicate := byID[document.DocumentID]; duplicate {
			return false, len(documents), ErrAgentAnnouncementInvalid
		}
		byID[document.DocumentID] = document
	}
	cited := make([]AgentAnnouncementResolvedDocument, 0, len(citations))
	seen := map[string]bool{}
	for _, id := range citations {
		document, ok := byID[id]
		if !ok || seen[id] {
			return false, len(documents), ErrAgentAnnouncementInvalid
		}
		seen[id] = true
		cited = append(cited, document)
	}
	audience := snapshot.CurrentMembers
	unreadable := 0
	for _, document := range cited {
		placed, placementErr := authority.OfficialConversationPlacement(ctx, tenant, conversationID, document.DocumentID, document.Version, document.Digest, document.SectionAnchor)
		if placementErr != nil {
			unreadable++
			continue
		}
		// AGENTUX-035: an official placement in this conversation speaks for its
		// members of this workspace, whom the hub serves the placement to. A
		// guest from another workspace is not among them and is still checked on
		// their own, like every member is for a document that is not placed here.
		toCheck := audience
		if placed {
			toCheck = nil
			for _, member := range audience {
				if member.TenantID != tenant {
					toCheck = append(toCheck, member)
				}
			}
			if len(toCheck) == 0 {
				continue
			}
		}
		if len(toCheck) > 200 {
			unreadable++
			continue
		}
		documentReadable := true
		checked := map[string]bool{}
		for _, member := range toCheck {
			key := member.TenantID + "\x00" + member.SubjectID
			if checked[key] {
				continue
			}
			checked[key] = true
			if authority.AuthorizeDocumentRead(ctx, tenant, document.DocumentID, document.Version, document.Digest, document.SectionAnchor, member.TenantID, member.SubjectID) != nil {
				documentReadable = false
				break
			}
		}
		if !documentReadable {
			unreadable++
		}
	}
	if unreadable != 0 {
		return false, unreadable, ErrAgentAnnouncementNotPublic
	}
	return true, 0, nil
}

var _ AgentAnnouncementPublicGate = AgentAnnouncementAudienceAuthority{}

func agentAnnouncementChannel(kind chat.ConversationKind) bool {
	return kind == chat.PublicChannel || kind == chat.PrivateChannel
}
