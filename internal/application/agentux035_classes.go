package application

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// AGENTUX-035: which classes of data a conversation allows is the
// conversation's own agent policy, the row an administrator sets for it. It is
// read each time it is asked, so narrowing a conversation's policy takes
// effect on the next answer.

// agentUX035ConversationPolicy reads a conversation's current agent policy.
type agentUX035ConversationPolicy interface {
	CapturePersonaChannelPolicy(context.Context, string, string, string) (chatstore.PersonaChannelPolicySnapshot, error)
}

// AgentUX035ConversationClasses answers from the conversation's agent policy.
type AgentUX035ConversationClasses struct {
	Policies agentUX035ConversationPolicy
}

// agentUX035Classes is the rule for the served composition, or nil when there
// is no chat store to read a policy from: with no rule no placement is enough.
func agentUX035Classes(store *chatstore.Store) AgentAnnouncementConversationClasses {
	if store == nil {
		return nil
	}
	return AgentUX035ConversationClasses{Policies: store}
}

// ConversationAllowsDataClass reports whether the conversation's current agent
// policy lists class. A conversation with no policy, one that is archived, one
// set to always answer privately, and one whose policy cannot be read allow
// nothing.
func (c AgentUX035ConversationClasses) ConversationAllowsDataClass(ctx context.Context, tenant, conversation, class string) (bool, error) {
	class = strings.ToUpper(strings.TrimSpace(class))
	if isNilPersonaOutputPort(c.Policies) || ctx == nil || strings.TrimSpace(tenant) == "" || strings.TrimSpace(conversation) == "" || class == "" {
		return false, nil
	}
	snapshot, err := c.Policies.CapturePersonaChannelPolicy(ctx, tenant, conversation, "")
	if errors.Is(err, dbport.ErrNoRows) || errors.Is(err, chatstore.ErrAudienceEligibilityUnavailable) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// A conversation whose agent answers are always private discloses nothing.
	if snapshot.TenantID != tenant || snapshot.ConversationID != conversation || snapshot.PolicyRevision <= 0 || snapshot.Policy.AlwaysPrivate {
		return false, nil
	}
	return slices.Contains(snapshot.Policy.AllowedDataClasses, class), nil
}
