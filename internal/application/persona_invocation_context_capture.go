package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaInvocationContextCapture = errors.New("application: persona invocation context capture unavailable")

type personaContextConversationReader interface {
	GetConversation(context.Context, chat.GetConversationRequest) (chat.Conversation, error)
}

type personaContextChatAudienceReader interface {
	CaptureAudienceSnapshot(context.Context, string, string) (chatstore.AudienceSnapshot, error)
}

// PersonaInvocationContextCapture is an invocation-scoped image of the posts
// currently visible to the invoker plus the exact audience snapshot used to
// reason about collaborative follow-up questions. Peer-authored bodies are
// deliberately absent; only their typed quarantine extraction is retained.
type PersonaInvocationContextCapture struct {
	Goal             string
	Posts            []PersonaInvocationContextPost
	ThreadSnapshotID string
	ThreadDigest     string
	Audience         PersonaChatAudienceSnapshot
}

// PersonaChatAudienceSnapshot preserves the exact reader-visible audience
// or the complete current and future public-channel population.
type PersonaChatAudienceSnapshot struct {
	ID, SnapshotID, Digest string
	Revision               uint64
	Current                []chatrecipient.AudiencePrincipal
	Eligible               []chatrecipient.AudiencePrincipal
}

// PersonaInvocationContextPost records the source and taint for one visible
// post. Body is populated only for posts authored by the verified invoker.
type PersonaInvocationContextPost struct {
	PostID, AuthorID string
	Digest           string
	Taint            string
	Body             string
	Extraction       agentinvoke.PeerExtraction
	Attachments      []PersonaInvocationContextAttachment
}

// PersonaInvocationContextAttachment records a referenced post attachment as
// untrusted data; its content is never loaded through a chat context read.
type PersonaInvocationContextAttachment struct {
	ID, Digest string
	Taint      string
}

// PersonaInvocationContextBuilder captures an atomic thread and audience
// image using server-owned chat adapters. It accepts neither caller-supplied
// visibility nor caller-supplied audience members.
type PersonaInvocationContextBuilder struct {
	Threads        PersonaThreadSnapshotSource
	Chat           personaContextConversationReader
	ChatStore      personaContextChatAudienceReader
	PublicAudience PersonaAudienceFloorSnapshotSource
	Peers          agentinvoke.PeerExtractor
}

// Capture reads the invoking post, visible thread and complete current chat
// audience from authenticated server-side sources. It fails closed when any
// image is missing, stale in shape, or does not contain the invoker.
func (b *PersonaInvocationContextBuilder) Capture(ctx context.Context, request agentinvoke.RunRequest) (PersonaInvocationContextCapture, error) {
	if b == nil || b.Threads == nil || b.Chat == nil || ctx == nil || !required(request.TenantID) || !required(request.ConversationID) ||
		!required(request.ThreadID) || !required(request.InvokingPostID) || !required(request.InvokerID) {
		return PersonaInvocationContextCapture{}, errPersonaInvocationContextCapture
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant().String() != request.TenantID || principal.Subject() != request.InvokerID {
		return PersonaInvocationContextCapture{}, errPersonaInvocationContextCapture
	}
	chatPrincipal := chat.Principal{TenantID: principal.Tenant().String(), SubjectID: principal.Subject(), Roles: principal.Roles()}
	thread, err := b.Threads.CaptureThreadSnapshot(ctx, chat.ThreadSnapshotRequest{
		Principal: chatPrincipal, TenantID: request.TenantID, ConversationID: request.ConversationID,
		ThreadID: request.ThreadID, InvokingPostID: request.InvokingPostID, Limit: chat.MaxThreadSnapshotPosts,
	})
	if err != nil || validatePersonaCapturedThread(thread, request, chatPrincipal) != nil {
		return PersonaInvocationContextCapture{}, fmt.Errorf("%w: current visible thread unavailable", errPersonaInvocationContextCapture)
	}
	conversation, err := b.Chat.GetConversation(ctx, chat.GetConversationRequest{Principal: chatPrincipal, TenantID: request.TenantID, ConversationID: request.ConversationID})
	if err != nil || conversation.ID != request.ConversationID || conversation.TenantID != request.TenantID || conversation.Revision == 0 || conversation.Archived {
		return PersonaInvocationContextCapture{}, fmt.Errorf("%w: current conversation unavailable", errPersonaInvocationContextCapture)
	}
	audience, err := b.captureAudience(ctx, request, conversation)
	if err != nil {
		return PersonaInvocationContextCapture{}, err
	}
	out := PersonaInvocationContextCapture{ThreadSnapshotID: thread.SnapshotID, ThreadDigest: thread.Digest, Audience: audience}
	for _, post := range thread.Posts {
		digest := digestPersonaThreadPost(agentinvoke.ThreadPost{Body: post.Body})
		entry := PersonaInvocationContextPost{PostID: post.ID, AuthorID: post.AuthorID, Digest: digest}
		for _, reference := range post.References {
			if reference.Kind == chat.MediaAttachment {
				entry.Attachments = append(entry.Attachments, PersonaInvocationContextAttachment{ID: reference.ID, Digest: digestPersonaThreadPost(agentinvoke.ThreadPost{Body: reference.ID}), Taint: agentinvoke.TaintUntrustedPeer})
			}
		}
		if post.ID == request.InvokingPostID {
			entry.Taint, entry.Body = agentinvoke.TaintInvokerInstruction, post.Body
			out.Goal = post.Body
		} else if post.AuthorID == request.InvokerID {
			entry.Taint, entry.Body = "INVOKER_HISTORY", post.Body
		} else {
			entry.Taint = agentinvoke.TaintUntrustedPeer
			if b.Peers == nil {
				return PersonaInvocationContextCapture{}, fmt.Errorf("%w: peer quarantine is required", errPersonaInvocationContextCapture)
			}
			entry.Extraction, err = b.Peers.Extract(ctx, agentinvoke.PeerExtractionRequest{PostID: post.ID, AuthorID: post.AuthorID, Digest: digest, Content: post.Body})
			if err != nil || entry.Extraction.SourceDigest != digest || strings.TrimSpace(entry.Extraction.SchemaID) == "" || strings.TrimSpace(entry.Extraction.SchemaVersion) == "" {
				return PersonaInvocationContextCapture{}, fmt.Errorf("%w: peer extraction is not bound to its visible post", errPersonaInvocationContextCapture)
			}
			entry.Extraction.Values = cloneStringMap(entry.Extraction.Values)
		}
		out.Posts = append(out.Posts, entry)
	}
	if strings.TrimSpace(out.Goal) == "" {
		return PersonaInvocationContextCapture{}, fmt.Errorf("%w: invoking post has no usable instruction", errPersonaInvocationContextCapture)
	}
	return out, nil
}

func (b *PersonaInvocationContextBuilder) captureAudience(ctx context.Context, request agentinvoke.RunRequest, conversation chat.Conversation) (PersonaChatAudienceSnapshot, error) {
	if conversation.Kind == chat.PublicChannel {
		if b.PublicAudience == nil {
			return PersonaChatAudienceSnapshot{}, errPersonaInvocationContextCapture
		}
		snapshot, err := b.PublicAudience.ReadPersonaAudienceFloorSnapshot(ctx, conversation)
		if err != nil || !validPersonaRunAudienceSnapshot(snapshot, conversation) {
			return PersonaChatAudienceSnapshot{}, fmt.Errorf("%w: complete public audience unavailable", errPersonaInvocationContextCapture)
		}
		digest, err := personaRunAudienceDigest(conversation, snapshot)
		if err != nil || !personaRunAudienceContainsInvoker(snapshot.CurrentMembers, agentinvoke.RunRequest{TenantID: request.TenantID, ConversationID: request.ConversationID, InvokerID: request.InvokerID}) {
			return PersonaChatAudienceSnapshot{}, fmt.Errorf("%w: public audience does not include invoker", errPersonaInvocationContextCapture)
		}
		return PersonaChatAudienceSnapshot{ID: conversation.ID, SnapshotID: "chat-audience-" + digest[len("sha256:"):], Digest: digest, Revision: snapshot.Revision,
			Current: cloneAudiencePrincipals(snapshot.CurrentMembers), Eligible: cloneAudiencePrincipals(snapshot.EligibleFutureMembers)}, nil
	}
	if b.ChatStore == nil {
		return PersonaChatAudienceSnapshot{}, errPersonaInvocationContextCapture
	}
	snapshot, err := b.ChatStore.CaptureAudienceSnapshot(ctx, request.TenantID, request.ConversationID)
	if err != nil || snapshot.TenantID != request.TenantID || snapshot.ConversationID != request.ConversationID || snapshot.Kind != string(conversation.Kind) ||
		snapshot.ConversationRevision == 0 || snapshot.PolicyRevision == 0 || len(snapshot.Members) == 0 || !personaRequestDigest(snapshot.Digest) || snapshot.SnapshotID != "chat-audience-"+snapshot.Digest {
		return PersonaChatAudienceSnapshot{}, fmt.Errorf("%w: exact chat audience unavailable", errPersonaInvocationContextCapture)
	}
	current := make([]chatrecipient.AudiencePrincipal, 0, len(snapshot.Members))
	foundInvoker := false
	seen := make(map[string]bool, len(snapshot.Members))
	for _, member := range snapshot.Members {
		key := member.HomeTenantID + "\x00" + member.MemberID
		if !required(member.HomeTenantID) || !required(member.MemberID) || member.Revision <= 0 || seen[key] {
			return PersonaChatAudienceSnapshot{}, fmt.Errorf("%w: malformed chat audience member", errPersonaInvocationContextCapture)
		}
		seen[key] = true
		current = append(current, chatrecipient.AudiencePrincipal{TenantID: member.HomeTenantID, SubjectID: member.MemberID, Guest: member.HomeTenantID != request.TenantID, External: member.External})
		foundInvoker = foundInvoker || member.HomeTenantID == request.TenantID && member.MemberID == request.InvokerID
	}
	if !foundInvoker {
		return PersonaChatAudienceSnapshot{}, fmt.Errorf("%w: invoker is not a current chat member", errPersonaInvocationContextCapture)
	}
	return PersonaChatAudienceSnapshot{ID: conversation.ID, SnapshotID: snapshot.SnapshotID, Digest: snapshot.Digest, Revision: uint64(snapshot.ConversationRevision), Current: current}, nil
}

func validatePersonaCapturedThread(snapshot chat.ThreadSnapshot, request agentinvoke.RunRequest, principal chat.Principal) error {
	if snapshot.TenantID != request.TenantID || snapshot.ConversationID != request.ConversationID || snapshot.ThreadID != request.ThreadID || snapshot.InvokingPostID != request.InvokingPostID ||
		snapshot.PrincipalTenantID != principal.TenantID || snapshot.PrincipalID != principal.SubjectID || !samePersonaThreadStrings(snapshot.PrincipalRoles, principal.Roles) ||
		!samePersonaThreadStrings(snapshot.PrincipalQualifications, principal.Qualifications) || snapshot.Revision == 0 || snapshot.AuthorityRevision == 0 ||
		len(snapshot.Posts) == 0 || len(snapshot.Posts) > chat.MaxThreadSnapshotPosts {
		return errPersonaInvocationContextCapture
	}
	digest, err := chat.ThreadSnapshotDigest(snapshot)
	if err != nil || digest != snapshot.Digest || snapshot.SnapshotID != "chat-thread-"+digest {
		return errPersonaInvocationContextCapture
	}
	foundInvoker := false
	seen := make(map[string]bool, len(snapshot.Posts))
	for _, post := range snapshot.Posts {
		if post.TenantID != request.TenantID || post.ConversationID != request.ConversationID || post.Deleted || (post.ID != request.ThreadID && post.ParentID != request.ThreadID) || !required(post.ID) || seen[post.ID] {
			return errPersonaInvocationContextCapture
		}
		seen[post.ID] = true
		if post.ID == request.InvokingPostID && post.AuthorID == request.InvokerID {
			foundInvoker = true
		}
	}
	if !foundInvoker {
		return errPersonaInvocationContextCapture
	}
	return nil
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
