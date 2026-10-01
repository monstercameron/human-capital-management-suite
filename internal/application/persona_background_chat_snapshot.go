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
)

var errPersonaBackgroundChatSnapshot = errors.New("application: current persona chat snapshot unavailable")

// PersonaBackgroundAudienceSnapshotSource reads tenant-scoped chat audience
// state without relying on an interactive caller context.
type PersonaBackgroundAudienceSnapshotSource interface {
	CaptureAudienceSnapshot(context.Context, string, string) (chatstore.AudienceSnapshot, error)
}

// PersonaBackgroundChatSnapshot is the current private audience and bounded
// thread image verified against the evidence captured at persona admission.
type PersonaBackgroundChatSnapshot struct {
	Audience chatstore.AudienceSnapshot
	Thread   chat.BackgroundThreadSnapshot
}

// PersonaBackgroundChatSnapshotReader rechecks a private persona invocation
// during admission or wake without fabricating a human trust principal. The
// original invoker ID is used only as the subject key in chat-store queries;
// current membership and post visibility are proved by the tenant-scoped
// store itself.
type PersonaBackgroundChatSnapshotReader struct {
	Audience PersonaBackgroundAudienceSnapshotSource
	Threads  chat.BackgroundThreadSnapshotSource
}

// ReadCurrent re-reads the exact private-thread context and fails closed when
// the conversation, membership, invoking post, or its revision has changed.
func (r PersonaBackgroundChatSnapshotReader) ReadCurrent(ctx context.Context, expected agentgate.PrivateChatScopeEvidence) (PersonaBackgroundChatSnapshot, error) {
	if r.Audience == nil || r.Threads == nil || ctx == nil || ctx.Err() != nil || !validPersonaBackgroundEvidence(expected) {
		return PersonaBackgroundChatSnapshot{}, errPersonaBackgroundChatSnapshot
	}
	tenant := expected.Tenant.String()
	audience, err := r.Audience.CaptureAudienceSnapshot(ctx, tenant, expected.ConversationID)
	if err != nil || !validPersonaBackgroundAudience(audience, expected) {
		return PersonaBackgroundChatSnapshot{}, fmt.Errorf("%w: current audience changed or unavailable", errPersonaBackgroundChatSnapshot)
	}
	thread, err := r.Threads.CaptureBackgroundThreadSnapshot(ctx, chat.BackgroundThreadSnapshotRequest{
		TenantID: tenant, ReaderID: expected.InvokerID, ConversationID: expected.ConversationID,
		ThreadID: expected.ThreadID, InvokingPostID: expected.InvokingPostID,
		Limit: chat.MaxThreadSnapshotPosts,
	})
	if err != nil || !validPersonaBackgroundThread(thread, expected) {
		return PersonaBackgroundChatSnapshot{}, fmt.Errorf("%w: invoking post changed or is no longer visible", errPersonaBackgroundChatSnapshot)
	}
	return PersonaBackgroundChatSnapshot{Audience: audience, Thread: thread}, nil
}

func validPersonaBackgroundEvidence(e agentgate.PrivateChatScopeEvidence) bool {
	return e.Allowed && e.Tenant.Validate() == nil && validPersonaBackgroundID(e.ConversationID) && validPersonaBackgroundID(e.ThreadID) &&
		validPersonaBackgroundID(e.InvokingPostID) && validPersonaBackgroundID(e.InvokerID) && e.InvokingPostAuthor == e.InvokerID &&
		e.PrivateConversation && e.ActiveMember && e.PostVisible && e.ConversationRev > 0 && e.MembershipRev > 0 && validPersonaPrivateDigest(e.PostDigest)
}

func validPersonaBackgroundAudience(s chatstore.AudienceSnapshot, expected agentgate.PrivateChatScopeEvidence) bool {
	tenant := expected.Tenant.String()
	if s.TenantID != tenant || s.ConversationID != expected.ConversationID || !privatePersonaConversation(chat.ConversationKind(s.Kind)) ||
		s.ConversationRevision <= 0 || uint64(s.ConversationRevision) != expected.ConversationRev || s.PolicyRevision <= 0 ||
		!validPersonaPrivateDigest(s.Digest) || s.SnapshotID != "chat-audience-"+s.Digest || len(s.Members) == 0 {
		return false
	}
	foundInvoker := false
	seen := make(map[string]struct{}, len(s.Members))
	for _, member := range s.Members {
		if !required(member.HomeTenantID) || !required(member.MemberID) || member.Revision <= 0 || member.External != (member.HomeTenantID != tenant) {
			return false
		}
		key := member.HomeTenantID + "\x00" + member.MemberID
		if _, ok := seen[key]; ok {
			return false
		}
		seen[key] = struct{}{}
		if member.HomeTenantID == tenant && member.MemberID == expected.InvokerID {
			if foundInvoker || member.External || uint64(member.Revision) != expected.MembershipRev {
				return false
			}
			foundInvoker = true
		}
	}
	return foundInvoker
}

func validPersonaBackgroundThread(s chat.BackgroundThreadSnapshot, expected agentgate.PrivateChatScopeEvidence) bool {
	tenant := expected.Tenant.String()
	if s.TenantID != tenant || s.ConversationID != expected.ConversationID || s.ThreadID != expected.ThreadID || s.InvokingPostID != expected.InvokingPostID ||
		s.ReaderTenantID != tenant || s.ReaderID != expected.InvokerID || s.Revision == 0 || s.AuthorityRevision != expected.ConversationRev ||
		len(s.Posts) == 0 || len(s.Posts) > chat.MaxThreadSnapshotPosts {
		return false
	}
	digest, err := chat.BackgroundThreadSnapshotDigest(s)
	if err != nil || digest != s.Digest || s.SnapshotID != "chat-background-thread-"+digest {
		return false
	}
	var invoking *chat.Post
	seen := make(map[string]struct{}, len(s.Posts))
	for i := range s.Posts {
		post := &s.Posts[i]
		if post.TenantID != tenant || post.ConversationID != expected.ConversationID || post.Deleted || !validPersonaBackgroundID(post.ID) ||
			(post.ID != expected.ThreadID && post.ParentID != expected.ThreadID) {
			return false
		}
		if _, ok := seen[post.ID]; ok {
			return false
		}
		seen[post.ID] = struct{}{}
		if post.ID == expected.InvokingPostID {
			if invoking != nil || post.AuthorID != expected.InvokerID || post.AuthorHomeTenantID != tenant || post.Revision == 0 {
				return false
			}
			invoking = post
		}
	}
	if invoking == nil {
		return false
	}
	encoded, err := json.Marshal(*invoking)
	if err != nil {
		return false
	}
	postDigest := sha256.Sum256(encoded)
	return strings.EqualFold(expected.PostDigest, "sha256:"+hex.EncodeToString(postDigest[:]))
}

func validPersonaBackgroundID(value string) bool {
	return strings.TrimSpace(value) != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00")
}
