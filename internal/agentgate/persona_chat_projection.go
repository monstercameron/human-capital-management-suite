package agentgate

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

// PersonaChatScopeAuthorizer proves the current invoker's exact chat tuple.
// Public evidence must include the revision of explicit admission policy;
// private evidence remains subject to the private conversation restriction.
type PersonaChatScopeAuthorizer interface {
	AuthorizePersonaChat(context.Context, PrivateChatScopeRequest) (PrivateChatScopeEvidence, error)
}

// ProjectPersonaChatSkillAuthorization admits only the exact T0 reply or
// policy-search pin. Public chat access is not permission to disclose: the
// complete current and future audience must still pass the output commit gate.
func (g *Gate) ProjectPersonaChatSkillAuthorization(ctx context.Context, req PrivateChatScopeRequest, chat PersonaChatScopeAuthorizer) (PrivateChatSkillAuthorization, error) {
	var resolve func(agentskills.SkillRecord) (capability.Key, bool)
	var scope string
	switch req.Skill.ID {
	case "persona.chat_reply":
		resolve, scope = privateReplyCapability, PrivateChatReplyScope
	case "hcmnext.skill.knowledge_search_with_citations":
		resolve, scope = privatePolicySearchCapability, "documents:search"
	default:
		return PrivateChatSkillAuthorization{}, &DeniedError{Code: DenyCapability, Skill: req.Skill.Key(), Detail: "skill has no persona chat domain projection"}
	}
	var authorize func(context.Context, PrivateChatScopeRequest) (PrivateChatScopeEvidence, error)
	if chat != nil {
		authorize = chat.AuthorizePersonaChat
	}
	return g.projectChatScope(ctx, req, authorize, personaChatEvidenceMatches, resolve, scope)
}

func personaChatEvidenceMatches(e PrivateChatScopeEvidence, req PrivateChatScopeRequest) bool {
	if e.PrivateConversation == e.PublicConversation {
		return false
	}
	return (!e.PublicConversation || e.PublicPolicyRev > 0) && chatEvidenceMatches(e, req)
}
