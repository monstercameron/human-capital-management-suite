package chat

import (
	"context"
	"errors"
)

// ErrAgentReactionsDisabled refuses an agent's reaction where an administrator
// turned reactions off: the workspace's switch, or the conversation's own when
// it has one (AGENTUX-052).
var ErrAgentReactionsDisabled = errors.New("chat: agent reactions are disabled")

// AgentReactionPolicy is whether agents may react to questions: one row for the
// workspace (conversation empty) and, where set, one per conversation. A row
// that was never stored means on.
type AgentReactionPolicy struct {
	ReactionsEnabled bool
}

// DefaultAgentReactionPolicy is the policy where nothing was stored: on.
func DefaultAgentReactionPolicy() AgentReactionPolicy {
	return AgentReactionPolicy{ReactionsEnabled: true}
}

// AgentReactionPolicyStore is the optional store port behind the workspace and
// conversation switches. Read answers the workspace row (the default when none
// was stored) and the conversation's own row when it has one.
type AgentReactionPolicyStore interface {
	ReadAgentReactionPolicy(ctx context.Context, tenant, conversation string) (AgentReactionPolicy, *AgentReactionPolicy, error)
	WriteAgentReactionPolicy(ctx context.Context, tenant, conversation string, p AgentReactionPolicy, by string) error
}

// agentReactionsEnabledByPolicy is the switches' answer for one conversation:
// the conversation's row wins, the workspace row is next, and a switch nobody
// set stays on. A switch that cannot be read stays on too: the reaction is
// cosmetic, and a lost setting must not become a silent agent, the same rule
// the persona owner's switch follows.
func agentReactionsEnabledByPolicy(ctx context.Context, store AgentReactionPolicyStore, tenant, conversation string) bool {
	workspace, channel, err := store.ReadAgentReactionPolicy(ctx, tenant, conversation)
	if err != nil {
		return true
	}
	if channel != nil {
		return channel.ReactionsEnabled
	}
	return workspace.ReactionsEnabled
}
