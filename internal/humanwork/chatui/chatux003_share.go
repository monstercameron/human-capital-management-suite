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
	// AgentShareRefused: the server said the answer stays private; Reason is
	// "agent" (the agent answers privately) or "audience" (not everyone in the
	// channel may open the sources).
	AgentShareRefused AgentShareStatus = "refused"
	// AgentShareFailed: the request did not complete; the person may try again.
	AgentShareFailed AgentShareStatus = "failed"
)

// AgentShareState is one answer's share state.
type AgentShareState struct {
	Status AgentShareStatus
	Reason string
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
	if state, ok := model.AgentShare[invocationID]; ok && (state.Status == AgentShareSharing || state.Status == AgentShareShared) {
		return
	}
	model.Callbacks.ShareAgentAnswer(invocationID)
}

// chatux003HandleAction handles the answer card's own actions. It reports
// whether the click was one of them.
func chatux003HandleAction(e ui.Event, model Model, local localUI, mentions mentionStore) bool {
	action, id, extra := eventAction(e)
	switch action {
	case "agent-follow-up":
		e.PreventDefault()
		chatux003AskFollowUp(model, mentions, id, extra)
		return true
	case "agent-share":
		e.PreventDefault()
		chatux003ShareClick(model, local, id)
		return true
	case "agent-ask-again":
		e.PreventDefault()
		chatux003AskAgain(model, id)
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

// chatux003AskAgainButton is the one action on a card whose answer failed or
// was interrupted with no run to retry: ask the same question again. It only
// ever asks on the person's own click.
func chatux003AskAgainButton(model Model, questionPostID string) ui.Node {
	label := chatux003Text(model, "chatux003.ask_again")
	return html.Button(html.Props{Class: "button secondary persona-progress-retry agent-ask-again", Type: "button", Disabled: model.Callbacks.SendMessageWithReferences == nil && model.Callbacks.SendMessage == nil, Aria: map[string]string{"label": label}, Data: map[string]string{"action": "agent-ask-again", "id": questionPostID}}, ui.Text(label))
}

// chatux003AskAgain sends the person's own question again, as a new message with
// the same text and the same agent mention.
func chatux003AskAgain(model Model, questionPostID string) {
	for _, post := range model.Messages {
		if post.ID != questionPostID || post.AuthorID != model.CurrentUser || model.CurrentUser == "" || strings.TrimSpace(post.Body) == "" {
			continue
		}
		if len(post.PersonaReferences) > 0 && model.Callbacks.SendMessageWithReferences != nil {
			model.Callbacks.SendMessageWithReferences(model.SelectedID, post.Body, post.PersonaReferences)
		} else if model.Callbacks.SendMessage != nil {
			model.Callbacks.SendMessage(model.SelectedID, post.Body)
		}
		return
	}
}
