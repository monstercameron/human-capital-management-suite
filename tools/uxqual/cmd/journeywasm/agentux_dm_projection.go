package main

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

type agentDirectIdentity struct {
	Icon     agenticon.Value
	Revision int64
	// Purpose is the agent's own description, the line under its name in the
	// conversation header. Empty means the source did not say; an earlier one is kept.
	Purpose string
}

// applyAgentDirectConversation adopts the identity projected by the
// authenticated persona directory. A chat member ID is not a human directory
// key, so direct-message presentation must not wait for worker lookup.
func applyAgentDirectConversation(model *chatui.Model, conversationID, agentID, name string, identity ...agentDirectIdentity) bool {
	if model == nil || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(agentID) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(name) == strings.TrimSpace(conversationID) || strings.TrimSpace(name) == strings.TrimSpace(agentID) {
		// An identifier is not a name; adopting it would make the row read as
		// an identifier once the agent flag hides the placeholder.
		return false
	}
	value := agenticon.Value{}
	revision := int64(0)
	purpose := ""
	if len(identity) > 0 {
		value, revision, purpose = identity[0].Icon, identity[0].Revision, strings.TrimSpace(identity[0].Purpose)
	}
	// A caller that knows only the name and agent id (the invocation and the
	// reload paths) must not erase an icon the directory already supplied.
	keepIcon := len(identity) == 0
	changed := false
	apply := func(conversation *chatui.Conversation) {
		if conversation == nil || conversation.ID != conversationID || conversation.Kind != chatui.DirectMessage {
			return
		}
		if keepIcon {
			value, revision = conversation.Icon, conversation.IconRevision
		}
		if conversation.Name != name || !conversation.Agent || conversation.AgentID != agentID || conversation.Icon != value || conversation.IconRevision != revision {
			conversation.Name, conversation.Agent, conversation.AgentID = name, true, agentID
			conversation.Icon, conversation.IconRevision = value, revision
			changed = true
		}
		if purpose != "" && conversation.AgentPurpose != purpose {
			conversation.AgentPurpose = purpose
			changed = true
		}
	}
	for index := range model.Conversations {
		apply(&model.Conversations[index])
	}
	for index := range model.Sections {
		for chatIndex := range model.Sections[index].Chats {
			apply(&model.Sections[index].Chats[chatIndex])
		}
	}
	for index := range model.Browse {
		apply(&model.Browse[index])
	}
	for index := range model.Members {
		member := &model.Members[index]
		if keepIcon {
			value, revision = member.Icon, member.IconRevision
		}
		if member.ID == agentID && (member.Name != name || !member.Agent || member.Icon != value || member.IconRevision != revision) {
			member.Name, member.Agent = name, true
			member.Icon, member.IconRevision = value, revision
			changed = true
		}
	}
	return changed
}

// agentDirectConversationTitle prefixes the shell title only for the selected
// agent DM. The caller owns the base title so tenant branding is retained.
func agentDirectConversationTitle(base string, model chatui.Model) string {
	for _, conversation := range model.Conversations {
		if conversation.ID == model.SelectedID && conversation.Kind == chatui.DirectMessage && conversation.Agent && strings.TrimSpace(conversation.Name) != "" {
			return strings.TrimSpace(conversation.Name) + " · " + strings.TrimSpace(base)
		}
	}
	return base
}
