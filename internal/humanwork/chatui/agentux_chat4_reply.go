package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The question's canonical reference is available before the directory or
// invocation read completes. It supplies attribution, never authority.
func agentReplyAuthor(model Model, postID, name string) string {
	if name = strings.TrimSpace(name); name != "" && name != personaProgressText(model, "chat.agent.name", "Agent") {
		return name
	}
	for _, messages := range [][]Message{model.Messages, model.ThreadMessages} {
		for _, question := range messages {
			if question.ID != postID {
				continue
			}
			for _, ref := range question.PersonaReferences {
				if ref.Kind == "AGENT_MENTION" && strings.TrimSpace(ref.Display) != "" {
					return strings.TrimSpace(ref.Display)
				}
			}
		}
	}
	if model.selected().Agent {
		return displayName(model, model.selected())
	}
	return ""
}

func agentReplyPending(part string) ui.Node {
	return html.Div(html.Props{Class: "agent-reply-pending agent-reply-pending-" + part, Aria: map[string]string{"busy": "true", "hidden": "true"}}, html.Span(html.Props{Class: "chat-skeleton"}))
}

func agentReplyNamedProjection(model Model, projection PersonaProgressProjection, postID string) PersonaProgressProjection {
	projection.AgentName = agentReplyAuthor(model, postID, projection.AgentName)
	return projection
}

func agentQuestionContextIsolated(model Model, envelope agentReplyEnvelope, clock string) ui.Node {
	if !envelope.QuestionAt.IsZero() {
		clock = chat5Clock(model.Locale, envelope.QuestionAt)
	}
	if envelope.QuestionLabel != "" {
		envelope.QuestionLabel = "\u2066" + envelope.QuestionLabel + "\u2069"
	}
	return renderAgentQuestionContext(model, envelope, clock)
}

func agentFollowUpLink(model Model, href, name string) ui.Node {
	return html.A(html.Props{Class: "button secondary agent-follow-up", Href: href, Data: map[string]string{"action": "agent-follow-up", "extra": href}, Aria: map[string]string{"label": strings.ReplaceAll(agentReplyFallback(model.Locale, "chat.agent.follow_up", "Ask {name} a follow-up"), "{name}", name)}, Text: agentUXChat4Text(model, "chat.agent.ask_follow_up")})
}

func agentProgressForPost(model Model, projection PersonaProgressProjection, postID string) ui.Node {
	projection.AgentName = agentReplyAuthor(model, postID, projection.AgentName)
	if projection.Progress != nil {
		progress := *projection.Progress
		progress.AgentName = projection.AgentName
		progress.QuestionPostID = postID
		projection.Progress = &progress
	}
	if projection.Failure != nil && projection.Failure.AskedAt.IsZero() {
		// CHATBUG-035: a retry offer expires with the question it belongs to.
		failure := *projection.Failure
		failure.AskedAt = agentQuestionSentAt(model, postID)
		projection.Failure = &failure
	}
	if projection.Failure != nil && projection.Failure.AskAgainPostID == "" {
		failure := *projection.Failure
		failure.AskAgainPostID = postID
		projection.Failure = &failure
	}
	if !model.selected().Agent {
		if projection.Failure != nil && projection.ViewerID != "" && projection.ViewerID == projection.InvokerID && projection.Failure.InvokerID == projection.InvokerID {
			return agentChannelFailure(model, projection, postID)
		}
		row := chat5ProgressFrame(model, projection)
		if projection.Progress != nil && projection.Progress.Visible && !projection.Progress.ResultReady && !projection.Progress.pastDeadline(time.Now()) && projection.ViewerID == projection.InvokerID && projection.Progress.InvokerID == projection.InvokerID {
			for _, child := range row.Children {
				if article, ok := child.(*ui.Element); ok && article.Type == "article" {
					article.Children = append([]any{agentChannelPrivacyLine(model, postID)}, article.Children...)
					break
				}
			}
		}
		return row
	}
	if projection.ViewerID == "" || projection.ViewerID != projection.InvokerID {
		return nil
	}
	if projection.Failure == nil {
		return chat5ProgressFrame(model, projection)
	}
	if projection.Failure.InvokerID != projection.InvokerID {
		return nil
	}
	// A failed direct answer is the agent's own message, with the same gutter
	// and metadata as a successful direct answer. There is no channel privacy
	// reminder or thread count inside this one-to-one conversation.
	failure := *projection.Failure
	meta := []ui.Node{html.Strong(html.Props{Class: "message-author", Text: projection.AgentName}), AgentBadgeLabel(model.Locale)}
	for _, msg := range model.Messages {
		if msg.ID == postID && msg.TimeLabel != "" {
			meta = append(meta, html.Time(html.Props{Class: "message-time", Text: msg.TimeLabel}))
			break
		}
	}
	content := []ui.Node{html.Div(html.Props{Class: "message-meta"}, meta...)}
	content = append(content, agentFailureContents(model, failure, projection.AgentName)...)
	return html.Article(html.Props{Class: "message agent-direct-state", Dir: agentReplyDirection(model.Locale), Data: map[string]string{"agent-reply-state": "failed"}}, agentDMAvatar(projection.AgentName, "avatar agent-dm-avatar", conversationAgentIcon(model, model.selected())), html.Div(html.Props{Class: "message-content"}, content...))
}

func agentChannelFailure(model Model, projection PersonaProgressProjection, postID string) ui.Node {
	failure := *projection.Failure
	// CHATBUG-035: the failure card carries one icon, the warning, so its privacy
	// line is words and a time only.
	children := []ui.Node{agentChannelPrivacyLineWith(model, postID, false), renderAgentReplyIdentity(model, projection.AgentName)}
	children = append(children, agentFailureContents(model, failure, projection.AgentName)...)
	return html.Article(html.Props{Class: "persona-progress-failure agent-reply-row", Dir: agentReplyDirection(model.Locale), Data: map[string]string{"agent-failure": "typed", "agent-reply-state": "failed"}}, children...)
}

func agentChannelPrivacyLine(model Model, postID string) ui.Node {
	return agentChannelPrivacyLineWith(model, postID, true)
}

// agentQuestionSentAt is when the question the invocation answers was sent, or
// the zero time when it is not on screen.
func agentQuestionSentAt(model Model, postID string) time.Time {
	for _, post := range append(append([]Message(nil), model.Messages...), model.ThreadMessages...) {
		if post.ID == postID && !post.SentAt.IsZero() {
			return post.SentAt
		}
	}
	return time.Time{}
}

func agentChannelPrivacyLineWith(model Model, postID string, withIcon bool) ui.Node {
	private := []ui.Node{}
	if withIcon {
		private = append(private, icon("eye"))
	}
	private = append(private, html.Span(html.Props{Class: "agent-reply-private-label", Text: personaProgressText(model, "chat.agent.only_visible", "Only visible to you")}))
	for _, post := range append(append([]Message(nil), model.Messages...), model.ThreadMessages...) {
		if post.ID == postID && post.TimeLabel != "" {
			private = append(private, html.Time(html.Props{Text: post.TimeLabel}))
			break
		}
	}
	return html.Div(html.Props{Class: "agent-reply-private"}, private...)
}

func permissionFailure(code string) bool {
	upper := strings.ToUpper(code)
	return strings.Contains(upper, "PERMISSION") || strings.Contains(upper, "AUTH") || strings.Contains(upper, "DENIED") || strings.Contains(upper, "GRANT") || upper == "ADMISSION_REFUSED"
}

// Keep the shared typed failure copy and retry semantics while arranging the
// surrounding attribution and privacy for the selected conversation.
func agentFailureContents(model Model, failure PersonaProgressFailure, name string) []ui.Node {
	card := renderPersonaProgressFailure(model, failure, name)
	var content []ui.Node
	for _, child := range card.Children {
		if node, ok := child.(*ui.Element); ok && (node.Type == "p" || node.Type == "span" || node.Type == "button") {
			content = append(content, node)
		}
	}
	if !failure.Retryable && permissionFailure(failure.Code) {
		content = append(content, html.Button(html.Props{Class: "button secondary agent-access-request", Type: "button", Disabled: model.Callbacks.SelectConversation == nil && model.Callbacks.OpenBrowse == nil, Data: map[string]string{"action": "agent-request-access"}, Text: agentUXChat4Text(model, "chat.agent.request_access")}))
	}
	return content
}

// IsLegacyPrivateAnswerReceipt identifies obsolete private-delivery notices.
func IsLegacyPrivateAnswerReceipt(body string) bool {
	return strings.TrimSpace(body) == legacyPrivateAnswerReceiptBody
}
