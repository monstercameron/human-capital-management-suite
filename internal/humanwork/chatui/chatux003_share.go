package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// AgentShareStatus is where one private answer stands after "Share to channel".
type AgentShareStatus string

const (
	// AgentShareSharing: the request is running.
	AgentShareSharing AgentShareStatus = "sharing"
	// AgentShareShared: the answer is now a message in the channel.
	AgentShareShared AgentShareStatus = "shared"
	// AgentShareRefused: the server said the answer stays private, and pressing
	// again cannot change that. Reason is "agent" (the agent answers privately),
	// "audience" (not everyone in the channel may open the sources; Source names
	// the one that is closed, when the server could), "expired" (the answer is too
	// old to share) or "denied" (only the person who asked may share it).
	AgentShareRefused AgentShareStatus = "refused"
	// AgentShareFailed: the request did not complete; the person may try again.
	AgentShareFailed AgentShareStatus = "failed"
	// AgentShareRemoving: the shared copy is being taken back (CHATUX-026).
	AgentShareRemoving AgentShareStatus = "removing"
)

// AgentShareState is one answer's share state.
type AgentShareState struct {
	Status AgentShareStatus
	Reason string
	// Source is the title of the document that keeps the answer private.
	Source string
	// PostID is the message the answer was shared as, once it is shared. "Remove
	// shared answer" deletes that message.
	PostID string
	// RemoveFailed is true when taking the shared copy back did not complete;
	// the copy is still in the channel.
	RemoveFailed bool
}

// chatux003Reason splits the line that names why an answer is private off its body.
func chatux003Reason(body string) (clean, reason string) {
	return chat.SplitPrivateReason(body)
}

// chatux003ChannelName is the conversation the card sits in, as the sentences
// name it, isolated so a right-to-left sentence does not reorder it.
func chatux003ChannelName(model Model) string {
	channel := model.selected()
	name := strings.TrimSpace(displayName(model, channel))
	if name == "" {
		return chatux003Text(model, "chatux003.channel_fallback")
	}
	return "⁨#" + strings.TrimPrefix(name, "#") + "⁩"
}

// chatux003FollowUpReference is the agent that can be mentioned in the
// conversation the card sits in, by canonical reference id.
func chatux003FollowUpReference(model Model, agentID string) (ChatReference, bool) {
	if agentID == "" || model.SelectedID == "" {
		return ChatReference{}, false
	}
	for _, candidate := range model.ResolvedPersonaMentions {
		reference := candidate.Reference
		if reference.ID == agentID && validResolvedPersonaReference(reference, model.SelectedID) && reference.TenantID == model.CurrentTenantID {
			return reference, true
		}
	}
	return ChatReference{}, false
}

// chatux003AskFollowUp is "Ask a follow-up": the composer of the conversation the
// card sits in, with the agent mentioned, ready to type. An agent that cannot be
// mentioned here falls back to the person's own conversation with it.
func chatux003AskFollowUp(model Model, mentions mentionStore, agentID, href string) {
	if reference, ok := chatux003FollowUpReference(model, agentID); ok {
		mentions.AddPersonaToken("chat-composer", model.SelectedID, reference)
		mentions.Set(mentionState{})
		focusAgentChatComposer()
		return
	}
	openAgentFollowUp(model, href)
}

// chatux003ShareClick starts sharing one answer. The state is set before the
// request, so the control reads "Sharing…" and cannot be pressed twice.
func chatux003ShareClick(model Model, local localUI, invocationID string) {
	if invocationID == "" || model.Callbacks.ShareAgentAnswer == nil {
		return
	}
	if state, ok := model.AgentShare[invocationID]; ok && (state.Status == AgentShareSharing || state.Status == AgentShareShared || state.Status == AgentShareRemoving) {
		return
	}
	model.Callbacks.ShareAgentAnswer(invocationID)
}

// chatux003HandleAction handles the answer card's own actions. It reports
// whether the click was one of them. Sharing has a question of its own before
// anything is sent and is handled by chatux026HandleAction.
func chatux003HandleAction(e ui.Event, model Model, local localUI, mentions mentionStore) bool {
	action, id, extra := eventAction(e)
	switch action {
	case "agent-follow-up":
		e.PreventDefault()
		chatux003AskFollowUp(model, mentions, id, extra)
		return true
	case "agent-question-jump":
		e.PreventDefault()
		chatbug047JumpToQuestion(model, id)
		return true
	case "agent-example":
		e.PreventDefault()
		chatux024UseExample(model, extra)
		return true
	}
	return false
}

// chatux003ChannelHint is the line under the composer when an agent is mentioned
// in a channel. An answer is posted to the channel for everyone unless the agent
// answers privately, a source is not open to every member, or the asker says so;
// the line says that before the question is sent.
func chatux003ChannelHint(model Model, mentions mentionStore, target, name string, channel Conversation) string {
	key := "chatux003.hint.public"
	if mentions.box != nil {
		for _, draft := range mentions.box.personas {
			if draft.Target != target || draft.ConversationID != model.SelectedID {
				continue
			}
			for _, candidate := range model.ResolvedPersonaMentions {
				if candidate.Reference.ID == draft.Reference.ID && candidate.ReplyPlacement == PersonaReplyPrivateAlways {
					key = "chatux003.hint.agent"
				}
			}
		}
	}
	return chatux003Format(model, key, map[string]string{"channel": "⁨#" + displayName(model, channel) + "⁩", "name": name})
}

// chatux003IsAnswer reports whether a message is a reply inside the open thread:
// an agent's answer to a question is posted there, under the question.
func chatux003IsAnswer(model Model, message Message) bool {
	if model.ThreadParentID == "" || message.ID == "" || message.ID == model.ThreadParentID {
		return false
	}
	for _, reply := range model.ThreadMessages {
		if reply.ID == message.ID {
			return true
		}
	}
	return false
}

// chatux003PersonName is the display name of a person known to this
// conversation, or "" when it cannot be established. An identifier is never
// shown in its place.
func chatux003PersonName(model Model, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if id == model.CurrentUser {
		return strings.TrimSpace(model.CurrentUserName)
	}
	for _, member := range model.Members {
		if member.ID == id && !member.Agent {
			return strings.TrimSpace(member.Name)
		}
	}
	return ""
}

// chatux003MessageBadge is the Agent badge on an agent's message. An answer,
// a reply under the question, adds who asked: "Agent asked by Cam".
func chatux003MessageBadge(model Model, message Message) ui.Node {
	actor := message.PersonaActor
	if model.selected().Agent || !actor.valid() || !chatux003IsAnswer(model, message) {
		return personaMessageBadge(model, actor)
	}
	badge := agentReplyFallback(model.Locale, "chat.agent.badge", "Agent")
	children := []ui.Node{html.Strong(html.Props{Text: badge})}
	if name := chatux003PersonName(model, actor.InvokerHandle); name != "" {
		children = append(children, html.Span(html.Props{Class: "agent-attribution", Dir: "auto", Text: chatux003Format(model, "chatux003.asked_by", map[string]string{"name": name})}))
	}
	return html.Span(html.Props{Class: "agent-badge", Data: map[string]string{"persona-id": actor.PersonaID, "agent-id": actor.AgentID}}, children...)
}
