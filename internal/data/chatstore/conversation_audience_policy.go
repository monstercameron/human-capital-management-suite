package chatstore

import (
	"context"
	"errors"
	"strings"

	chat "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CreateConversationWithAudiencePolicy creates a private channel, direct
// message, or group conversation together with its explicitly supplied policy
// in one chat database transaction. Policy facts are caller-owned inputs; the
// store never infers classification, residency, role requirements, or audience
// restrictions from a room name or its current members.
func (s *Adapter) CreateConversationWithAudiencePolicy(ctx context.Context, c chat.Conversation, members []chat.Membership, key string, policy AudiencePolicy) (chat.Conversation, error) {
	if s == nil || s.Store == nil || !audiencePolicyConversation(string(c.Kind)) {
		return chat.Conversation{}, errors.New("audience policy creation requires a private, direct, or group conversation")
	}
	if strings.TrimSpace(policy.Classification) == "" || (policy.RoleMode != 1 && policy.RoleMode != 2) {
		return chat.Conversation{}, errors.New("explicit audience classification and valid role mode are required")
	}
	if c.OwnerID == "" || !containsConversationOwner(c, members) {
		return chat.Conversation{}, errors.New("conversation owner must be an explicit same-tenant member")
	}
	return s.createConversation(ctx, c, members, key, &policy)
}

func containsConversationOwner(c chat.Conversation, members []chat.Membership) bool {
	for _, member := range members {
		if member.TenantID == c.TenantID && member.HomeTenantID == c.TenantID && member.SubjectID == c.OwnerID && member.Role == chat.Manager {
			return true
		}
	}
	return false
}
