package chat

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type AgentReactionChoice struct{ Key, Emoji, Hint string }

// AgentReactionChoices returns a copy of the fixed work-safe vocabulary.
func AgentReactionChoices() []AgentReactionChoice {
	return []AgentReactionChoice{
		{"check", "✅", "answered"}, {"beach", "🏖️", "time off"}, {"cake", "🎂", "birthday"}, {"money", "💰", "pay"},
		{"calendar", "📅", "schedule"}, {"clock", "🕒", "hours"}, {"document", "📄", "policy"}, {"book", "📚", "learning"},
		{"bulb", "💡", "idea"}, {"tools", "🛠️", "tools"}, {"handshake", "🤝", "agreement"}, {"welcome", "👋", "welcome"},
		{"star", "⭐", "recognition"}, {"trophy", "🏆", "achievement"}, {"target", "🎯", "goal"}, {"chart", "📊", "report"},
		{"clipboard", "📋", "checklist"}, {"folder", "📁", "files"}, {"search", "🔎", "find"}, {"compass", "🧭", "guidance"},
		{"shield", "🛡️", "safety"}, {"lock", "🔒", "security"}, {"key", "🔑", "access"}, {"house", "🏠", "remote work"},
		{"building", "🏢", "office"}, {"laptop", "💻", "technology"}, {"globe", "🌍", "locations"}, {"plane", "✈️", "travel"},
		{"train", "🚆", "commute"}, {"seedling", "🌱", "growth"}, {"heart", "💚", "wellbeing"}, {"medical", "🩺", "health"},
		{"umbrella", "☂️", "coverage"}, {"balance", "⚖️", "fairness"}, {"puzzle", "🧩", "solution"}, {"rocket", "🚀", "launch"},
		{"bell", "🔔", "reminder"}, {"mail", "📨", "message"}, {"coffee", "☕", "break"}, {"celebrate", "🎉", "milestone"},
	}
}

func AgentAnswerReaction(key string, publicPrivate bool) string {
	if !publicPrivate {
		for _, choice := range AgentReactionChoices() {
			if choice.Key == key {
				return choice.Emoji
			}
		}
	}
	return "✅"
}

func ValidAgentReactionKey(key string) bool {
	for _, choice := range AgentReactionChoices() {
		if key == choice.Key {
			return true
		}
	}
	return false
}

// Binding comes from the current invocation owner, never from message text or
// caller-provided target IDs. PublicPrivate prevents public topic disclosure.
type AgentReactionBinding struct {
	TenantID, ConversationID, InvokingPostID, AgentSubjectID string
	Enabled, PublicPrivate                                   bool
}

type AgentReactionAuthority interface {
	ResolveAgentReaction(context.Context, Principal, string, string, string) (AgentReactionBinding, error)
}

func (s *Service) SetAgentReactionAuthority(authority AgentReactionAuthority) {
	s.agentReactionAuthority = authority
}

func (s *Service) authorizeAgentReaction(ctx context.Context, p Principal, tenant, conversation, post, emoji string, removing bool) error {
	verified, authenticated := trust.FromContext(ctx)
	if !authenticated || verified == nil || verified.SubjectKind() != trust.SubjectKindAgent && verified.SubjectKind() != trust.SubjectKindIntegration {
		return nil
	}
	if !machineConversationPrincipal(ctx, p) || s.agentReactionAuthority == nil {
		return ErrPermissionDenied
	}
	binding, err := s.agentReactionAuthority.ResolveAgentReaction(ctx, p, tenant, conversation, post)
	if err != nil || binding.TenantID != tenant || binding.ConversationID != conversation || binding.InvokingPostID != post || binding.AgentSubjectID != p.SubjectID || p.TenantID != tenant {
		return ErrPermissionDenied
	}
	if removing {
		return nil
	}
	if !binding.Enabled || binding.PublicPrivate && emoji != "👀" && emoji != "✅" {
		return ErrPermissionDenied
	}
	if emoji == "👀" {
		return nil
	}
	for _, choice := range AgentReactionChoices() {
		if choice.Emoji == emoji {
			return nil
		}
	}
	return ErrInvalidArgument
}
