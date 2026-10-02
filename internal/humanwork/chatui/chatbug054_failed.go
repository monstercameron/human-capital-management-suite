package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// CHATBUG-054: a failed answer used to be a line of its own for "Only visible
// to you", the agent, a sentence, and for an older question no control at all.
// It is now the answered card's layout: one header line that ends in the
// visibility note, the reason in one plain sentence, and one row of actions
// that always holds "Ask again" and "Dismiss" for the person who asked.

// chatbug054Header is the failed card's header line: the answered card's
// header, with the time of the question it failed to answer (there is no answer
// to date) and the visibility note in words only, because the card's one icon
// is the warning (CHATBUG-035).
func chatbug054Header(model Model, name, questionPostID string) ui.Node {
	identity := storedAgentIcon(model, nil, name)
	if !identity.Valid() && model.selected().Agent {
		identity = model.selected().Icon
	}
	avatar := agentDMAvatar(name, "avatar small agent-reply-avatar", agentIconFor(model, nil, name, identity))
	head := append(chatux017Identity(model, chatux003AgentID(model, PersonaPostActor{}, name), name, avatar), AgentBadgeLabel(model.Locale))
	if question, found := chatbug040MessageByID(model, questionPostID); found && questionPostID != "" && question.TimeLabel != "" {
		head = append(head, html.Time(html.Props{Text: question.TimeLabel}))
	}
	note := personaProgressText(model, "chat.agent.only_visible", "Only visible to you")
	head = append(head, html.Span(html.Props{Class: "agent-reply-private", Data: map[string]string{"visibility": "private"}}, html.Span(html.Props{Class: "agent-reply-private-label", Text: note})))
	return html.Header(html.Props{Class: "agent-reply-identity agent-reply-head"}, head...)
}

// chatbug054Reason is the reason, one plain sentence that is its own live
// region. A failure that asking again cannot mend adds what would, in muted
// text; one that it can says nothing more, because the button says it.
func chatbug054Reason(model Model, failure PersonaProgressFailure, name string) []ui.Node {
	description := chat.AgentAnswerFailureFor(model.Locale, name, failure.Code)
	if failure.Code == chat.AgentMentionLimitCode {
		description = chat.AgentAnswerMentionLimit(model.Locale, name, failure.MentionsAsked, failure.RetryAt.Local())
	}
	nodes := []ui.Node{html.P(html.Props{Class: "agent-failure-heading", Role: "status", Aria: map[string]string{"live": "polite", "atomic": "true"}}, icon("warning"), html.Span(html.Props{Dir: "auto", Text: description.Sentence}))}
	if !description.Retryable && strings.TrimSpace(description.NextStep) != "" {
		nodes = append(nodes, html.P(html.Props{Class: "agent-failure-next", Dir: "auto", Text: description.NextStep}))
	}
	return nodes
}

// chatbug054RetryTarget is what "Ask again" names to the server: the run that
// failed, or, when no run stands behind the card, the question itself.
func chatbug054RetryTarget(model Model, failure PersonaProgressFailure) string {
	// A question refused by the hourly limit would be refused again: no way to
	// ask again is offered until the time the card names.
	if failure.Code == chat.AgentMentionLimitCode && time.Now().Before(failure.RetryAt) {
		return ""
	}
	if id := strings.TrimSpace(failure.InvocationID); id != "" && !strings.HasPrefix(id, "pending:") {
		return id
	}
	if failure.AskAgainPostID != "" && model.SelectedID != "" {
		return "question:" + model.SelectedID + ":" + failure.AskAgainPostID
	}
	return ""
}

// chatbug054DismissKey names the card for "Dismiss".
func chatbug054DismissKey(failure PersonaProgressFailure) string {
	if id := strings.TrimSpace(failure.InvocationID); id != "" && !strings.HasPrefix(id, "pending:") {
		return id
	}
	if failure.AskAgainPostID != "" {
		return "question:" + failure.AskAgainPostID
	}
	return ""
}

// chatbug054Actions is the action row of a failed card: "Ask again", "Dismiss",
// then any further action the kind of failure calls for. It is nil when the
// card names nothing that could be asked again (a card drawn outside a
// conversation) and has no further action.
func chatbug054Actions(model Model, failure PersonaProgressFailure, further ...ui.Node) ui.Node {
	var row []ui.Node
	if target := chatbug054RetryTarget(model, failure); target != "" {
		again := chatux003Text(model, "chatux003.ask_again")
		dismiss := chatbug054Text(model.Locale, "dismiss")
		row = append(row,
			html.Button(html.Props{Class: "button secondary persona-progress-retry agent-ask-again", Type: "button", Aria: map[string]string{"label": again}, Data: map[string]string{"agent-action": "retry", "agent-invocation-id": target, "agent-question": failure.AskAgainPostID}}, ui.Text(again)),
			html.Button(html.Props{Class: "agent-feedback-button agent-reply-action agent-reply-dismiss", Type: "button", Aria: map[string]string{"label": dismiss}, Data: map[string]string{"agent-action": "dismiss", "agent-invocation-id": chatbug054DismissKey(failure)}}, ui.Text(dismiss)))
	}
	row = append(row, further...)
	if len(row) == 0 {
		return nil
	}
	if retry, asked := model.AgentRetries[failure.AskAgainPostID]; asked && failure.AskAgainPostID != "" && chatbug047NotAsked(retry, time.Now()) {
		row = append(row, html.Span(html.Props{Class: "agent-reply-share-note agent-retry-note", Role: "status", Dir: "auto", Text: chatbug054Text(model.Locale, "not_asked")}))
	}
	return html.Div(html.Props{Class: "agent-reply-footer agent-reply-actions agent-failure-actions"}, row...)
}

// chatbug054Dismissed reports whether the person dismissed this failed card.
func chatbug054Dismissed(model Model, failure PersonaProgressFailure) bool {
	key := chatbug054DismissKey(failure)
	return key != "" && model.AgentDismissed[key]
}

func chatbug054Text(locale, key string) string {
	copy := map[string][3]string{
		"dismiss":   {"Dismiss", "Ausblenden", "إخفاء"},
		"not_asked": {"The question could not be asked again. Try again in a moment.", "Die Frage konnte nicht erneut gestellt werden. Versuchen Sie es gleich noch einmal.", "تعذر طرح السؤال مرة أخرى. حاول بعد قليل."},
	}
	return chatbug039Text(key, copy[key][chatbug039LocaleIndex(locale)], copy[key][0])
}

// chatBug054Styles gives the failed card the answered card's frame and spacing.
const chatBug054Styles = `.persona-progress-failure.agent-reply-row{box-sizing:border-box;width:100%;max-width:var(--chat-measure);margin-inline:0;padding:10px 12px;border:1px solid var(--hcm-color-border);border-inline-start:3px solid var(--hcm-color-warning);border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface)}` +
	`.persona-progress-failure .agent-failure-heading{margin:6px 0 0}` +
	`.agent-failure-next{margin:4px 0 0;color:var(--muted);font-size:.8125rem;line-height:1.4}` +
	`.agent-failure-actions .persona-progress-retry{margin-top:0}` +
	`.agent-direct-state .agent-failure-actions{margin-block-start:8px}`
