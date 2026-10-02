package chat

import (
	"context"
	"strings"
)

// AGENTUX-075: an agent that is asked a question reacts to that question at
// once. The reaction is the agent's own (its subject is the agent that was
// asked), it can only land on the message of the person who asked, and its
// emoji comes from the small fixed set below, never from model output.

// AgentQuestionReaction is one reaction by an agent to the message that asked
// it. The caller has already proven that the agent is the one the question was
// put to; this method proves the rest.
type AgentQuestionReaction struct {
	TenantID, ConversationID, PostID string
	// AgentSubjectID is the agent that reacts. It need not be a member of the
	// conversation: it may react wherever it may answer.
	AgentSubjectID string
	// Asker is the person who posted the question. The question must be theirs
	// and visible to them.
	Asker Principal
	Emoji string
	// Replaces, when set, is an emoji of the agent's own on the same question that
	// is taken away as this one is put (AGENTUX-052: the reaction that fits the
	// outcome takes the place of the first). It must be in the fixed set too.
	Replaces string
}

// Outcome reactions (AGENTUX-052): what the agent puts on the question once it
// has finished, chosen from the outcome and never from what was asked, so a
// public channel learns nothing about a private answer's topic.

// AgentOutcomeAnswered is the reaction to a question that was answered.
func AgentOutcomeAnswered() string { return "✅" }

// AgentOutcomeCouldNotAnswer is the reaction when the agent found nothing it may
// use, or was not allowed to answer.
func AgentOutcomeCouldNotAnswer() string { return "🤷" }

// AgentOutcomeFailed is the reaction when the run failed.
func AgentOutcomeFailed() string { return string([]rune{0x26A0, 0xFE0F}) }

// AgentQuestionReactionEmoji lists the emoji an agent may put on a question: one
// per kind of question before it answers, and one per outcome after.
func AgentQuestionReactionEmoji() []string {
	return []string{"👀", "🔎", "🙏", "🙌", "🫡", "🛠️", "👋", AgentOutcomeAnswered(), AgentOutcomeCouldNotAnswer(), AgentOutcomeFailed()}
}

// ValidAgentQuestionReactionEmoji reports whether emoji is one of that set.
func ValidAgentQuestionReactionEmoji(emoji string) bool {
	for _, allowed := range AgentQuestionReactionEmoji() {
		if emoji == allowed {
			return true
		}
	}
	return false
}

// AgentQuestionReactionCommitter is the optional chat port that stores such a
// reaction.
type AgentQuestionReactionCommitter interface {
	CommitAgentQuestionReaction(context.Context, AgentQuestionReaction) (Reaction, error)
}

// AgentQuestionReactionStore is the optional store port that lets an agent that
// is not a member of the conversation put and take away its own reaction.
type AgentQuestionReactionStore interface {
	PutAgentQuestionReaction(context.Context, Reaction) (Reaction, error)
	RemoveAgentQuestionReaction(ctx context.Context, tenant, conversation, post, home, subject, emoji string) error
}

// CommitAgentQuestionReaction stores the agent's reaction to the question that
// asked it. It refuses an emoji outside the fixed set, a message the asker did
// not write, and a message the asker cannot see.
func (s *Service) CommitAgentQuestionReaction(ctx context.Context, r AgentQuestionReaction) (Reaction, error) {
	if err := validatePrincipal(r.Asker, r.TenantID); err != nil {
		return Reaction{}, err
	}
	if r.ConversationID == "" || r.PostID == "" || strings.TrimSpace(r.AgentSubjectID) == "" || r.AgentSubjectID == r.Asker.SubjectID ||
		r.Asker.TenantID != r.TenantID || !ValidAgentQuestionReactionEmoji(r.Emoji) ||
		(r.Replaces != "" && !ValidAgentQuestionReactionEmoji(r.Replaces)) {
		return Reaction{}, ErrInvalidArgument
	}
	if err := s.requireVisiblePost(ctx, r.Asker, r.TenantID, r.ConversationID, r.PostID); err != nil {
		return Reaction{}, err
	}
	post, err := s.store.GetPost(ctx, r.TenantID, r.ConversationID, r.PostID)
	if err != nil {
		return Reaction{}, err
	}
	if post.AuthorID != r.Asker.SubjectID {
		return Reaction{}, ErrPermissionDenied
	}
	reaction := Reaction{
		TenantID: r.TenantID, ConversationID: r.ConversationID, PostID: r.PostID,
		SubjectID: r.AgentSubjectID, HomeTenantID: r.TenantID, Emoji: r.Emoji, CreatedAt: s.now(),
	}
	agentStore, direct := s.store.(AgentQuestionReactionStore)
	if !direct {
		stored, err := s.store.PutReaction(ctx, reaction)
		if err == nil && r.Replaces != "" && r.Replaces != r.Emoji {
			err = s.store.RemoveReaction(ctx, r.TenantID, r.ConversationID, r.PostID, r.TenantID, r.AgentSubjectID, r.Replaces)
		}
		return stored, err
	}
	stored, err := agentStore.PutAgentQuestionReaction(ctx, reaction)
	if err != nil {
		return Reaction{}, err
	}
	if r.Replaces != "" && r.Replaces != r.Emoji {
		if err := agentStore.RemoveAgentQuestionReaction(ctx, r.TenantID, r.ConversationID, r.PostID, r.TenantID, r.AgentSubjectID, r.Replaces); err != nil {
			return stored, err
		}
	}
	return stored, nil
}
