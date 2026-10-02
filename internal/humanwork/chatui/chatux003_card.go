package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
)

// chatux003AnswerCard is an agent's private answer in a channel, built so the
// answer is read first: one header line (icon, name, Agent badge, time, and at
// its end, in muted text, who can see it), the answer, one line saying why it is
// private when there is a reason, the sources as chips, and one row of actions.
func chatux003AnswerCard(model Model, local localUI, message EphemeralMessage, projection PersonaProgressProjection) ui.Node {
	name := strings.TrimSpace(projection.AgentName)
	actor, hasActor := model.PersonaPostActors[message.ID]
	if hasActor && actor.Actor.valid() && strings.TrimSpace(actor.Display) != "" {
		name = strings.TrimSpace(actor.Display)
	}
	name = agentReplyAuthor(model, message.ThreadID, name)
	body, reason := chatux003Reason(message.Body)
	marker := personaProgressText(model, "chat.agent.only_visible", "Only visible to you")
	envelope := bindAgentQuestionThreadLink(parseAgentReplyEnvelope(body), message.ThreadLink)
	envelope.Body = readerReplyBody(model, Message{ID: message.ID, Body: body}, envelope.Body)
	identity := model.selected().Icon
	if hasActor && actor.Actor.valid() {
		identity = actor.Actor.Icon
		if !identity.Valid() {
			identity = storedAgentIcon(model, []string{actor.Actor.PersonaID, actor.Actor.AgentID}, name)
		}
	}
	children := []ui.Node{
		chatux003Header(model, name, identity, message.CreatedAt, marker),
		html.Span(html.Props{Class: "sr-only agent-reply-announcement", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}, Text: marker}),
	}
	if context := agentQuestionContextIsolated(model, envelope, ""); context != nil && model.selected().Agent {
		children = append(children, context)
	}
	children = append(children, html.Div(html.Props{Class: "agent-reply-answer", Dir: "auto"}, markdownMessageBody(model, envelope.Body)...))
	if why := chatux003Why(model, reason, name); why != "" {
		children = append(children, html.P(html.Props{Class: "agent-reply-why", Dir: "auto", Text: why}))
	}
	children = append(children, renderAgentReplySources(model, envelope)...)
	feedbackInvocation := ""
	if projection.InvocationID != "" && !strings.HasPrefix(projection.InvocationID, "pending:") {
		feedbackInvocation = projection.InvocationID
	}
	menuID := "agent-card:" + message.ID
	actions := chatux003Actions(model, local, message, projection, name, reason, feedbackInvocation, menuID, actor)
	children = append(children, actions)
	if model.MenuID == menuID {
		if menu := chatux003Menu(model, projection, name, menuID); menu != nil {
			children = append(children, menu)
		}
	}
	return html.Article(html.Props{Class: "chat-ephemeral agent-reply-row", Dir: agentReplyDirection(model.Locale), Data: map[string]string{"ephemeral-id": message.ID, "ephemeral-thread": message.ThreadID, "agent-reply-state": "answered-private"}}, children...)
}

// chatux003Header is the card's one header line.
func chatux003Header(model Model, name string, identity agenticon.Value, at time.Time, note string) ui.Node {
	if name = strings.TrimSpace(name); name == "" {
		name = personaProgressText(model, "chat.agent.name", "Agent")
	}
	if !identity.Valid() {
		identity = storedAgentIcon(model, nil, name)
	}
	if !identity.Valid() && model.selected().Agent {
		identity = model.selected().Icon
	}
	avatar := agentDMAvatar(name, "avatar small agent-reply-avatar", agentIconFor(model, nil, name, identity))
	head := []ui.Node{avatar, html.Strong(html.Props{Class: "agent-reply-name", Text: name}), AgentBadgeLabel(model.Locale)}
	if clock := agentReplyTime(model, at); clock != nil {
		head = append(head, clock)
	}
	head = append(head, html.Span(html.Props{Class: "agent-reply-private", Data: map[string]string{"visibility": "private"}}, icon("eye"), html.Span(html.Props{Class: "agent-reply-private-label", Text: note})))
	return html.Header(html.Props{Class: "agent-reply-identity agent-reply-head"}, head...)
}

// chatux003Why is the one plain line that says why an answer is private, or ""
// when the server named no reason (an answer delivered before reasons were
// recorded, or in a direct conversation).
func chatux003Why(model Model, reason, name string) string {
	switch reason {
	case "asked", "agent", "audience":
		return chatux003Format(model, "chatux003.why."+reason, map[string]string{"name": name, "channel": chatux003ChannelName(model)})
	}
	return ""
}

// chatux003Actions is the one action row: Helpful, Not right, Ask a follow-up,
// and Share to channel for a private answer that may be shared, with the saved
// copy's link in the card's more menu. The row keeps its follow-up while the
// agent exists, even when the answer cannot be rated.
func chatux003Actions(model Model, local localUI, message EphemeralMessage, projection PersonaProgressProjection, name, reason, invocationID, menuID string, actor PersonaPostActor) ui.Node {
	row := []ui.Node{}
	if invocationID != "" {
		feedbackModel := model
		projection.DurablePostID = message.ID
		feedbackModel.PersonaInvocations = []PersonaThreadInvocation{{Projection: projection}}
		if feedback := renderAgentFeedback(feedbackModel, local, message.ID); feedback != nil {
			row = append(row, feedback)
		}
	}
	href := strings.TrimSpace(projection.PrivateReplyHref)
	agentID := chatux003AgentID(model, actor, name)
	followUp := strings.ReplaceAll(agentReplyFallback(model.Locale, "chat.agent.follow_up", "Ask {name} a follow-up"), "{name}", name)
	row = append(row, html.Button(html.Props{Class: "agent-feedback-button agent-reply-action agent-follow-up", Type: "button", Data: map[string]string{"action": "agent-follow-up", "id": agentID, "extra": href}, Aria: map[string]string{"label": followUp}, Title: followUp},
		icon("reply"), html.Span(html.Props{Text: agentUXChat4Text(model, "chat.agent.ask_follow_up")})))
	row = append(row, chatux003ShareControls(model, reason, name, invocationID)...)
	if href != "" {
		more := chatux003Text(model, "chatux003.more")
		row = append(row, html.Button(html.Props{Class: "message-action agent-reply-action agent-reply-more", Type: "button", Hidden: model.Callbacks.OpenMenu == nil, Disabled: model.Callbacks.OpenMenu == nil, Data: map[string]string{"action": "menu", "id": menuID}, Aria: map[string]string{"label": more, "haspopup": "menu", "expanded": boolString(model.MenuID == menuID)}, Title: more}, icon("more")))
	}
	return html.Div(html.Props{Class: "agent-reply-footer agent-reply-actions"}, row...)
}

// chatux003AgentID is the canonical reference id of the agent that answered, from
// the trusted actor of the answer, or from the one agent the person can mention
// here under that name. Empty when it cannot be established.
func chatux003AgentID(model Model, actor PersonaPostActor, name string) string {
	if actor.Actor.valid() {
		for _, candidate := range model.ResolvedPersonaMentions {
			if candidate.Reference.ID == actor.Actor.AgentID {
				return candidate.Reference.ID
			}
		}
	}
	found := ""
	for _, candidate := range model.ResolvedPersonaMentions {
		if strings.EqualFold(strings.TrimSpace(candidate.Reference.Display), strings.TrimSpace(name)) && name != "" {
			if found != "" && found != candidate.Reference.ID {
				return ""
			}
			found = candidate.Reference.ID
		}
	}
	return found
}

// chatux003ShareControls is the share control or, once it has been used, what
// came of it. An agent that answers privately never offers to share; a control
// that cannot work here is not shown.
func chatux003ShareControls(model Model, reason, name, invocationID string) []ui.Node {
	if invocationID == "" || reason == "agent" || model.Callbacks.ShareAgentAnswer == nil || model.selected().Kind != PublicChannel {
		return nil
	}
	channel := chatux003ChannelName(model)
	state := model.AgentShare[invocationID]
	note := func(key string) ui.Node {
		return html.Span(html.Props{Class: "agent-reply-share-note", Role: "status", Dir: "auto", Text: chatux003Format(model, key, map[string]string{"name": name, "channel": channel})})
	}
	switch state.Status {
	case AgentShareShared:
		return []ui.Node{note("chatux003.shared")}
	case AgentShareRefused:
		if state.Reason == "agent" {
			return []ui.Node{note("chatux003.share.refused.agent")}
		}
		return []ui.Node{note("chatux003.share.refused.audience")}
	}
	label := chatux003Text(model, "chatux003.share")
	sharing := state.Status == AgentShareSharing
	if sharing {
		label = chatux003Text(model, "chatux003.sharing")
	}
	hint := chatux003Format(model, "chatux003.share.hint", map[string]string{"channel": channel})
	button := html.Button(html.Props{Class: "agent-feedback-button agent-reply-action agent-reply-share", Type: "button", Disabled: sharing, Data: map[string]string{"action": "agent-share", "id": invocationID}, Aria: map[string]string{"label": label + ". " + hint, "busy": boolString(sharing)}, Title: hint},
		icon("send"), html.Span(html.Props{Text: label}))
	if state.Status == AgentShareFailed {
		return []ui.Node{button, note("chatux003.share.failed")}
	}
	return []ui.Node{button}
}

// chatux003Menu is the card's more menu: the link to the copy saved in the
// person's own conversation with the agent.
func chatux003Menu(model Model, projection PersonaProgressProjection, name, menuID string) ui.Node {
	href := strings.TrimSpace(projection.PrivateReplyHref)
	if href == "" {
		return nil
	}
	label := personaProgressText(model, "chat.agent.saved_conversation", "Saved in your conversation with") + " " + name
	return anchoredChatLayer(html.Props{Class: "message-menu agent-reply-menu", Role: "menu", Data: map[string]string{"message-menu": menuID}, Aria: map[string]string{"label": chatux003Text(model, "chatux003.more")}}, "menu",
		html.A(html.Props{Class: "menu-item agent-reply-open", Role: "menuitem", Href: href, Text: label}))
}

// ChatUX003Styles lays the card out as one header line, the answer, source chips
// and one row of actions, using the product's tokens only.
const ChatUX003Styles = `.agent-reply-head{display:flex;align-items:center;gap:8px;min-width:0;flex-wrap:wrap}` +
	`.agent-reply-head .agent-reply-name{flex:0 1 auto;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.agent-reply-head time{color:var(--muted);font-size:.8125rem;font-variant-numeric:tabular-nums;white-space:nowrap}` +
	`.agent-reply-head .agent-reply-private{display:inline-flex;align-items:center;gap:4px;margin-inline-start:auto;color:var(--muted);font-size:.8125rem;white-space:nowrap}` +
	`.agent-reply-head .agent-reply-private .chat-icon{width:14px;height:14px}` +
	`.agent-reply-answer{margin-block-start:6px}` +
	`.agent-reply-why{margin:6px 0 0;color:var(--muted);font-size:.8125rem;line-height:1.4}` +
	`.agent-reply-sources{display:flex;align-items:center;gap:6px 8px;flex-wrap:wrap;margin-block:8px 0;padding:0;border:0}` +
	`.agent-reply-sources h4{flex:none;margin:0;color:var(--muted);font-size:.8125rem;font-weight:600}` +
	`.agent-reply-sources ul{display:flex;flex-wrap:wrap;gap:6px;margin:0;padding:0;list-style:none;min-width:0}` +
	`.agent-reply-source{display:inline-flex;align-items:center;gap:5px;max-width:100%;min-width:0;padding:2px 10px;border:1px solid var(--line);border-radius:999px;background:var(--surface);font-size:.8125rem;line-height:1.5}` +
	`.agent-reply-source .chat-icon{width:14px;height:14px;flex:none;color:var(--muted)}` +
	`.agent-reply-source a,.agent-reply-source bdi{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.agent-reply-source a{color:var(--accent);text-decoration:none}.agent-reply-source:has(a):hover,.agent-reply-source:has(a:focus-visible){border-color:var(--accent)}.agent-reply-source a:hover{text-decoration:underline}` +
	`.agent-reply-source-locked{position:relative;background:var(--soft);color:var(--muted);cursor:default}` +
	`.agent-reply-source-locked .agent-reply-source-unavailable{position:absolute;inset-block-end:calc(100% + 6px);inset-inline-start:0;z-index:5;display:block;grid-column:auto;width:max-content;max-width:min(260px,70vw);padding:4px 8px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font-size:.75rem;line-height:1.4;white-space:normal;box-shadow:var(--hcm-shadow-raised);opacity:0;pointer-events:none}` +
	`.agent-reply-source-locked:hover .agent-reply-source-unavailable,.agent-reply-source-locked:focus-visible .agent-reply-source-unavailable{opacity:1}` +
	`.agent-reply-actions{display:flex;flex-wrap:wrap;align-items:center;gap:6px 8px;margin-block-start:10px;padding:0;border:0}` +
	`.agent-reply-actions .agent-feedback{display:flex;flex-wrap:wrap;gap:6px 8px;margin:0}` +
	`.agent-reply-action{display:inline-flex;align-items:center;gap:5px;min-height:32px;padding-inline:10px;border:1px solid var(--line);border-radius:999px;background:var(--surface);color:var(--ink);font:inherit;font-size:.8125rem;cursor:pointer}` +
	`.agent-reply-action .chat-icon{width:16px;height:16px;flex:none}.agent-reply-action:hover,.agent-reply-action:focus-visible{border-color:var(--accent);color:var(--accent)}.agent-reply-action:disabled{color:var(--muted);cursor:default}` +
	`.agent-reply-actions .agent-reply-more{width:auto;min-width:32px;height:auto;margin-inline-start:auto;justify-content:center;padding-inline:6px;border:1px solid var(--line);background:var(--surface);color:var(--ink)}` +
	`.agent-reply-share-note{color:var(--muted);font-size:.8125rem}` +
	`.agent-reply-menu .menu-item{display:flex;align-items:center;gap:8px;text-decoration:none;color:var(--ink)}` +
	`@media(max-width:480px){.agent-reply-head .agent-reply-private{margin-inline-start:0;flex-basis:100%}}` +
	`.agent-progress-cancel{flex:none;white-space:nowrap}.agent-reply-state-line{flex-wrap:wrap;row-gap:2px}.agent-reply-state-copy{flex:0 1 auto;min-width:0;max-width:calc(100% - 24px)}` +
	`@media(max-width:480px){.agent-reply-state-divider,.agent-progress-cancel{order:1}.agent-reply-counter{order:2;flex:1 0 100%;margin-inline-start:0}}` +
	`@media(pointer:coarse){.agent-reply-action{min-height:44px}}`
