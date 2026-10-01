package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaPrivateChatScope = errors.New("application: private chat scope unavailable")

type personaPrivateChatConversationReader interface {
	GetConversation(context.Context, chat.GetConversationRequest) (chat.Conversation, error)
}

type personaPrivateChatAudienceReader interface {
	CaptureAudienceSnapshot(context.Context, string, string) (chatstore.AudienceSnapshot, error)
}

type personaPrivateChatThreadReader interface {
	CaptureThreadSnapshot(context.Context, chat.ThreadSnapshotRequest) (chat.ThreadSnapshot, error)
}

// PersonaPrivateChatScopeAuthorizer resolves current chat authority for one
// verified invoker, conversation, thread, and invoking post.
type PersonaPrivateChatScopeAuthorizer struct {
	conversations personaPrivateChatConversationReader
	audience      personaPrivateChatAudienceReader
	threads       personaPrivateChatThreadReader
}

var _ agentgate.PrivateChatScopeAuthorizer = (*PersonaPrivateChatScopeAuthorizer)(nil)

// NewPersonaPrivateChatScopeAuthorizer requires the current conversation,
// audience, and reader-authorized thread sources. The audience and thread
// snapshots must share chatstore's audience revision fence.
func NewPersonaPrivateChatScopeAuthorizer(conversations personaPrivateChatConversationReader, audience personaPrivateChatAudienceReader, threads personaPrivateChatThreadReader) (*PersonaPrivateChatScopeAuthorizer, error) {
	if conversations == nil || audience == nil || threads == nil {
		return nil, errPersonaPrivateChatScope
	}
	return &PersonaPrivateChatScopeAuthorizer{conversations: conversations, audience: audience, threads: threads}, nil
}

// AuthorizePrivateChat rereads the private conversation, active membership,
// and visible invoking post. It denies if the independently captured chat
// snapshots do not agree on the current audience revision.
func (a *PersonaPrivateChatScopeAuthorizer) AuthorizePrivateChat(ctx context.Context, request agentgate.PrivateChatScopeRequest) (agentgate.PrivateChatScopeEvidence, error) {
	if a == nil || a.conversations == nil || a.audience == nil || a.threads == nil {
		return agentgate.PrivateChatScopeEvidence{}, errPersonaPrivateChatScope
	}
	verified, ok := personaChatScopePrincipal(ctx, request)
	if !ok {
		return agentgate.PrivateChatScopeEvidence{}, errPersonaPrivateChatScope
	}
	tenant, invoker := verified.Tenant().String(), verified.Subject()
	principal := chat.Principal{TenantID: tenant, SubjectID: invoker, Roles: verified.Roles()}
	conversation, err := a.conversations.GetConversation(ctx, chat.GetConversationRequest{
		Principal: principal, TenantID: tenant, ConversationID: request.ConversationID,
	})
	if err != nil || conversation.ID != request.ConversationID || conversation.TenantID != tenant || conversation.Archived || !privatePersonaConversation(conversation.Kind) {
		return agentgate.PrivateChatScopeEvidence{}, errPersonaPrivateChatScope
	}
	audience, err := a.audience.CaptureAudienceSnapshot(ctx, tenant, request.ConversationID)
	if err != nil || !validPrivatePersonaAudience(audience, tenant, request.ConversationID, conversation.Kind) {
		return agentgate.PrivateChatScopeEvidence{}, errPersonaPrivateChatScope
	}
	thread, err := a.threads.CaptureThreadSnapshot(ctx, chat.ThreadSnapshotRequest{
		Principal: principal, TenantID: tenant, ConversationID: request.ConversationID,
		ThreadID: request.ThreadID, InvokingPostID: request.InvokingPostID, Limit: chat.MaxThreadSnapshotPosts,
	})
	if err != nil || !validPrivatePersonaThread(thread, tenant, request, principal) || thread.AuthorityRevision != uint64(audience.ConversationRevision) {
		return agentgate.PrivateChatScopeEvidence{}, errPersonaPrivateChatScope
	}
	memberRevision, memberOK := privatePersonaMembershipRevision(audience, tenant, invoker)
	post, postOK := privatePersonaInvokingPost(thread, request, tenant, invoker)
	if !memberOK || !postOK {
		return agentgate.PrivateChatScopeEvidence{}, errPersonaPrivateChatScope
	}
	digest, err := privatePersonaPostDigest(post)
	if err != nil {
		return agentgate.PrivateChatScopeEvidence{}, errPersonaPrivateChatScope
	}
	return agentgate.PrivateChatScopeEvidence{
		Allowed: true, Tenant: request.Tenant, InvokerID: invoker, ConversationID: request.ConversationID,
		ThreadID: request.ThreadID, InvokingPostID: request.InvokingPostID, InvokingPostAuthor: invoker,
		PrivateConversation: true, ActiveMember: true, PostVisible: true,
		ConversationRev: uint64(audience.ConversationRevision), MembershipRev: memberRevision,
		PostDigest: digest, EvaluatedAt: request.At,
	}, nil
}

func personaChatScopePrincipal(ctx context.Context, request agentgate.PrivateChatScopeRequest) (*trust.Principal, bool) {
	if ctx == nil || ctx.Err() != nil ||
		request.Tenant.Validate() != nil || !required(request.ConversationID) || !required(request.ThreadID) || !required(request.InvokingPostID) || request.Purpose == "" || request.At.IsZero() {
		return nil, false
	}
	verified, ok := trust.FromContext(ctx)
	if !ok || verified == nil || verified.SubjectKind() != trust.SubjectKindHuman || verified.Tenant() != request.Tenant ||
		verified.Subject() == "" || !verified.AuthorizesPurpose(request.Purpose) || request.User.Principal == nil ||
		request.User.Principal.SubjectKind() != trust.SubjectKindHuman || request.User.Principal.Tenant() != verified.Tenant() ||
		request.User.Principal.Subject() != verified.Subject() || request.At.Before(verified.IssuedAt()) || !request.At.Before(verified.ExpiresAt()) {
		return nil, false
	}
	return verified, true
}

func privatePersonaConversation(kind chat.ConversationKind) bool {
	switch kind {
	case chat.PrivateChannel, chat.Direct, chat.Group:
		return true
	default:
		return false
	}
}

func validPrivatePersonaAudience(snapshot chatstore.AudienceSnapshot, tenant, conversation string, kind chat.ConversationKind) bool {
	return snapshot.TenantID == tenant && snapshot.ConversationID == conversation && strings.EqualFold(snapshot.Kind, string(kind)) &&
		!strings.EqualFold(snapshot.Kind, string(chat.PublicChannel)) && snapshot.ConversationRevision > 0 && snapshot.PolicyRevision > 0 &&
		validPersonaPrivateDigest(snapshot.Digest) && snapshot.SnapshotID == "chat-audience-"+snapshot.Digest && len(snapshot.Members) > 0
}

func validPrivatePersonaThread(snapshot chat.ThreadSnapshot, tenant string, request agentgate.PrivateChatScopeRequest, principal chat.Principal) bool {
	digest, err := chat.ThreadSnapshotDigest(snapshot)
	return err == nil && snapshot.TenantID == tenant && snapshot.ConversationID == request.ConversationID &&
		snapshot.ThreadID == request.ThreadID && snapshot.InvokingPostID == request.InvokingPostID &&
		snapshot.PrincipalTenantID == principal.TenantID && snapshot.PrincipalID == principal.SubjectID &&
		snapshot.Revision > 0 && snapshot.AuthorityRevision > 0 && snapshot.Digest == digest &&
		snapshot.SnapshotID == "chat-thread-"+digest
}

func privatePersonaMembershipRevision(snapshot chatstore.AudienceSnapshot, tenant, invoker string) (uint64, bool) {
	var revision uint64
	found := false
	for _, member := range snapshot.Members {
		if member.HomeTenantID != tenant || member.MemberID != invoker {
			continue
		}
		if found || member.Revision <= 0 || member.External {
			return 0, false
		}
		revision, found = uint64(member.Revision), true
	}
	return revision, found
}

func privatePersonaInvokingPost(snapshot chat.ThreadSnapshot, request agentgate.PrivateChatScopeRequest, tenant, invoker string) (chat.Post, bool) {
	var found chat.Post
	for _, post := range snapshot.Posts {
		if post.ID != request.InvokingPostID {
			continue
		}
		if found.ID != "" || post.TenantID != tenant || post.ConversationID != request.ConversationID ||
			post.AuthorID != invoker || post.AuthorHomeTenantID != tenant || post.Revision == 0 || post.Deleted ||
			(post.ID != request.ThreadID && post.ParentID != request.ThreadID) {
			return chat.Post{}, false
		}
		found = post
	}
	return found, found.ID != ""
}

func privatePersonaPostDigest(post chat.Post) (string, error) {
	encoded, err := json.Marshal(post)
	if err != nil {
		return "", fmt.Errorf("encode invoking post digest: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validPersonaPrivateDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range value[len("sha256:"):] {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f')) {
			return false
		}
	}
	return true
}
