package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecipient"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var errPersonaRunAudienceSnapshot = errors.New("application: persona run audience snapshot unavailable")

type personaRunAudienceConversationReader interface {
	GetConversation(context.Context, chat.GetConversationRequest) (chat.Conversation, error)
}

type personaRunChatAudienceSnapshotReader interface {
	CaptureAudienceSnapshot(context.Context, string, string) (chatstore.AudienceSnapshot, error)
}

// PersonaRunAudienceSnapshotSource resolves a canonical audience scope from
// an atomic, tenant-scoped authority read. Public channels use the complete
// current and eligible population; private conversations use chatstore's
// fenced active-membership and policy snapshot.
type PersonaRunAudienceSnapshotSource struct {
	Chat      personaRunAudienceConversationReader
	ChatStore personaRunChatAudienceSnapshotReader
	Snapshot  PersonaAudienceFloorSnapshotSource
}

var _ personaRunAudienceScopeSource = (*PersonaRunAudienceSnapshotSource)(nil)

type personaRunAudienceScopeSource interface {
	ResolvePersonaRunAudience(context.Context, agentinvoke.RunRequest) (agentrun.AudienceScope, error)
}

// ResolvePersonaRunAudience captures one complete audience image and derives
// its ID and digest from server-owned snapshot data. Caller-provided
// membership, revision, or digest values are never accepted.
func (s *PersonaRunAudienceSnapshotSource) ResolvePersonaRunAudience(ctx context.Context, invocation agentinvoke.RunRequest) (agentrun.AudienceScope, error) {
	if s == nil || s.Chat == nil || ctx == nil || strings.TrimSpace(invocation.TenantID) == "" || strings.TrimSpace(invocation.ConversationID) == "" || strings.TrimSpace(invocation.InvokerID) == "" {
		return agentrun.AudienceScope{}, errPersonaRunAudienceSnapshot
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.SubjectKind() != trust.SubjectKindHuman || principal.Tenant().String() != invocation.TenantID || principal.Subject() != invocation.InvokerID {
		return agentrun.AudienceScope{}, errPersonaRunAudienceSnapshot
	}
	conversation, err := s.Chat.GetConversation(ctx, chat.GetConversationRequest{
		Principal: chat.Principal{TenantID: invocation.TenantID, SubjectID: invocation.InvokerID},
		TenantID:  invocation.TenantID, ConversationID: invocation.ConversationID,
	})
	if err != nil || conversation.TenantID != invocation.TenantID || conversation.ID != invocation.ConversationID || conversation.Revision == 0 || conversation.Archived {
		return agentrun.AudienceScope{}, fmt.Errorf("%w: current conversation unavailable", errPersonaRunAudienceSnapshot)
	}
	if conversation.Kind != chat.PublicChannel {
		return s.resolvePrivatePersonaRunAudience(ctx, invocation, conversation)
	}
	if s.Snapshot == nil {
		return agentrun.AudienceScope{}, errPersonaRunAudienceSnapshot
	}
	snapshot, err := s.Snapshot.ReadPersonaAudienceFloorSnapshot(ctx, conversation)
	if err != nil || !validPersonaRunAudienceSnapshot(snapshot, conversation) || !personaRunAudienceContainsInvoker(snapshot.CurrentMembers, invocation) {
		return agentrun.AudienceScope{}, fmt.Errorf("%w: incomplete or unfenced authority snapshot", errPersonaRunAudienceSnapshot)
	}
	digest, err := personaRunAudienceDigest(conversation, snapshot)
	if err != nil {
		return agentrun.AudienceScope{}, fmt.Errorf("%w: canonicalize snapshot: %v", errPersonaRunAudienceSnapshot, err)
	}
	return agentrun.AudienceScope{ID: conversation.ID, SnapshotID: "chat-audience-" + digest[len("sha256:"):], Digest: digest}, nil
}

func (s *PersonaRunAudienceSnapshotSource) resolvePrivatePersonaRunAudience(ctx context.Context, invocation agentinvoke.RunRequest, conversation chat.Conversation) (agentrun.AudienceScope, error) {
	if s.ChatStore == nil {
		return agentrun.AudienceScope{}, errPersonaRunAudienceSnapshot
	}
	snapshot, err := s.ChatStore.CaptureAudienceSnapshot(ctx, invocation.TenantID, invocation.ConversationID)
	if err != nil || snapshot.TenantID != invocation.TenantID || snapshot.ConversationID != invocation.ConversationID ||
		strings.EqualFold(snapshot.Kind, string(chat.PublicChannel)) || !strings.EqualFold(snapshot.Kind, string(conversation.Kind)) ||
		snapshot.ConversationRevision == 0 || snapshot.PolicyRevision == 0 || len(snapshot.Members) == 0 ||
		!personaRequestDigest(snapshot.Digest) || snapshot.SnapshotID != "chat-audience-"+snapshot.Digest {
		return agentrun.AudienceScope{}, fmt.Errorf("%w: incomplete or mismatched chat audience snapshot", errPersonaRunAudienceSnapshot)
	}
	for _, member := range snapshot.Members {
		if !required(member.HomeTenantID) || !required(member.MemberID) || member.Revision <= 0 {
			return agentrun.AudienceScope{}, fmt.Errorf("%w: malformed chat audience member", errPersonaRunAudienceSnapshot)
		}
	}
	invokerPresent := false
	for _, member := range snapshot.Members {
		if member.HomeTenantID == invocation.TenantID && member.MemberID == invocation.InvokerID {
			invokerPresent = true
			break
		}
	}
	if !invokerPresent {
		return agentrun.AudienceScope{}, fmt.Errorf("%w: invoker is no longer a current member", errPersonaRunAudienceSnapshot)
	}
	return agentrun.AudienceScope{ID: conversation.ID, SnapshotID: snapshot.SnapshotID, Digest: snapshot.Digest}, nil
}

func personaRunAudienceContainsInvoker(members []chatrecipient.AudiencePrincipal, invocation agentinvoke.RunRequest) bool {
	for _, member := range members {
		if member.TenantID == invocation.TenantID && member.SubjectID == invocation.InvokerID {
			return true
		}
	}
	return false
}

func validPersonaRunAudienceSnapshot(snapshot PersonaAudienceFloorSnapshot, conversation chat.Conversation) bool {
	return conversation.Kind == chat.PublicChannel && snapshot.Revision > 0 && snapshot.CurrentComplete && snapshot.EligibilityComplete && snapshot.GuestExternalComplete && snapshot.FenceComplete &&
		len(snapshot.CurrentMembers) > 0 && len(snapshot.EligibleFutureMembers) > 0 &&
		validAudiencePrincipals(snapshot.CurrentMembers) && validAudiencePrincipals(snapshot.EligibleFutureMembers) && consistentPersonaRunAudience(snapshot.CurrentMembers, snapshot.EligibleFutureMembers)
}

func consistentPersonaRunAudience(current, eligible []chatrecipient.AudiencePrincipal) bool {
	known := make(map[string]chatrecipient.AudiencePrincipal, len(current))
	for _, member := range current {
		known[member.TenantID+"\x00"+member.SubjectID] = member
	}
	for _, member := range eligible {
		if previous, ok := known[member.TenantID+"\x00"+member.SubjectID]; ok && (previous.Guest != member.Guest || previous.External != member.External) {
			return false
		}
	}
	return true
}

func personaRunAudienceDigest(conversation chat.Conversation, snapshot PersonaAudienceFloorSnapshot) (string, error) {
	current := canonicalAudienceMembers(snapshot.CurrentMembers)
	eligible := canonicalAudienceMembers(snapshot.EligibleFutureMembers)
	image := struct {
		TenantID, ConversationID string
		Revision                 uint64
		Current, Eligible        []canonicalPersonaAudienceMember
	}{conversation.TenantID, conversation.ID, snapshot.Revision, current, eligible}
	raw, err := json.Marshal(image)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

type canonicalPersonaAudienceMember struct {
	TenantID  string
	SubjectID string
	Guest     bool
	External  bool
}

func canonicalAudienceMembers(members []chatrecipient.AudiencePrincipal) []canonicalPersonaAudienceMember {
	out := make([]canonicalPersonaAudienceMember, 0, len(members))
	for _, member := range members {
		out = append(out, canonicalPersonaAudienceMember{TenantID: member.TenantID, SubjectID: member.SubjectID, Guest: member.Guest, External: member.External})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TenantID != out[j].TenantID {
			return out[i].TenantID < out[j].TenantID
		}
		return out[i].SubjectID < out[j].SubjectID
	})
	return out
}
