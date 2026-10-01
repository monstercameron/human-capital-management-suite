package application

import (
	"context"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

var errPersonaChatScope = errors.New("application: current persona chat scope unavailable")

type personaChatScopeAudienceReader interface {
	personaPrivateChatAudienceReader
	CapturePublicAudienceSnapshot(context.Context, string, string) (chatstore.PublicAudienceSnapshot, error)
}

// PersonaChatScopeAuthorizer reads the current conversation owner and exact
// visible invoking post. Public rooms require explicit complete admission
// authority; private rooms retain their existing membership restriction.
type PersonaChatScopeAuthorizer struct {
	private  *PersonaPrivateChatScopeAuthorizer
	audience personaChatScopeAudienceReader
}

var _ agentgate.PersonaChatScopeAuthorizer = (*PersonaChatScopeAuthorizer)(nil)

func NewPersonaChatScopeAuthorizer(conversations personaPrivateChatConversationReader, audience personaChatScopeAudienceReader, threads personaPrivateChatThreadReader) (*PersonaChatScopeAuthorizer, error) {
	if isNilPersonaOutputPort(conversations) || isNilPersonaOutputPort(audience) || isNilPersonaOutputPort(threads) {
		return nil, errPersonaChatScope
	}
	private, err := NewPersonaPrivateChatScopeAuthorizer(conversations, audience, threads)
	if err != nil {
		return nil, err
	}
	return &PersonaChatScopeAuthorizer{private: private, audience: audience}, nil
}

func (a *PersonaChatScopeAuthorizer) AuthorizePersonaChat(ctx context.Context, req agentgate.PrivateChatScopeRequest) (agentgate.PrivateChatScopeEvidence, error) {
	if a == nil || a.private == nil || isNilPersonaOutputPort(a.audience) {
		return agentgate.PrivateChatScopeEvidence{}, errPersonaChatScope
	}
	verified, ok := personaChatScopePrincipal(ctx, req)
	if !ok {
		return agentgate.PrivateChatScopeEvidence{}, errPersonaChatScope
	}
	tenant, invoker := verified.Tenant().String(), verified.Subject()
	principal := chat.Principal{TenantID: tenant, SubjectID: invoker, Roles: verified.Roles()}
	conversation, err := a.private.conversations.GetConversation(ctx, chat.GetConversationRequest{Principal: principal, TenantID: tenant, ConversationID: req.ConversationID})
	if err != nil || conversation.TenantID != tenant || conversation.ID != req.ConversationID || conversation.Archived {
		return agentgate.PrivateChatScopeEvidence{}, errPersonaChatScope
	}
	if privatePersonaConversation(conversation.Kind) {
		return a.private.AuthorizePrivateChat(ctx, req)
	}
	if conversation.Kind != chat.PublicChannel {
		return agentgate.PrivateChatScopeEvidence{}, errPersonaChatScope
	}
	audience, err := a.audience.CapturePublicAudienceSnapshot(ctx, tenant, req.ConversationID)
	if err != nil || !validPersonaPublicChatScopeAudience(audience, tenant, req.ConversationID, invoker) {
		return agentgate.PrivateChatScopeEvidence{}, errPersonaChatScope
	}
	thread, err := a.private.threads.CaptureThreadSnapshot(ctx, chat.ThreadSnapshotRequest{Principal: principal, TenantID: tenant,
		ConversationID: req.ConversationID, ThreadID: req.ThreadID, InvokingPostID: req.InvokingPostID, Limit: chat.MaxThreadSnapshotPosts})
	if err != nil || !validPrivatePersonaThread(thread, tenant, req, principal) || thread.AuthorityRevision != audience.Revision {
		return agentgate.PrivateChatScopeEvidence{}, errPersonaChatScope
	}
	post, ok := privatePersonaInvokingPost(thread, req, tenant, invoker)
	if !ok {
		return agentgate.PrivateChatScopeEvidence{}, errPersonaChatScope
	}
	digest, err := privatePersonaPostDigest(post)
	if err != nil {
		return agentgate.PrivateChatScopeEvidence{}, errPersonaChatScope
	}
	return agentgate.PrivateChatScopeEvidence{Allowed: true, Tenant: req.Tenant, InvokerID: invoker, ConversationID: req.ConversationID,
		ThreadID: req.ThreadID, InvokingPostID: req.InvokingPostID, InvokingPostAuthor: invoker, PublicConversation: true,
		PublicPolicyRev: uint64(audience.PolicyRevision), ActiveMember: true, PostVisible: true, ConversationRev: audience.Revision,
		// Public audience_revision advances with membership and admission changes.
		MembershipRev: audience.Revision, PostDigest: digest, EvaluatedAt: req.At}, nil
}

func validPersonaPublicChatScopeAudience(snapshot chatstore.PublicAudienceSnapshot, tenant, conversation, invoker string) bool {
	if snapshot.TenantID != tenant || snapshot.ConversationID != conversation || snapshot.Revision == 0 || snapshot.PolicyRevision <= 0 || len(snapshot.Current) == 0 || len(snapshot.Eligible) == 0 {
		return false
	}
	eligible := make(map[[2]string]chatstore.PublicAudiencePrincipal, len(snapshot.Eligible))
	for _, member := range snapshot.Eligible {
		key := [2]string{member.HomeTenantID, member.SubjectID}
		if !required(member.HomeTenantID) || !required(member.SubjectID) {
			return false
		}
		if _, duplicate := eligible[key]; duplicate {
			return false
		}
		eligible[key] = member
	}
	current := make(map[[2]string]struct{}, len(snapshot.Current))
	invokerPresent := false
	for _, member := range snapshot.Current {
		key := [2]string{member.HomeTenantID, member.SubjectID}
		admitted, ok := eligible[key]
		if !ok || admitted.Guest != member.Guest {
			return false
		}
		if _, duplicate := current[key]; duplicate {
			return false
		}
		current[key] = struct{}{}
		if member.HomeTenantID == tenant && member.SubjectID == invoker && !member.Guest {
			invokerPresent = true
		}
	}
	return invokerPresent
}
