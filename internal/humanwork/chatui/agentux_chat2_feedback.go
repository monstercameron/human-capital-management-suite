package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The two ratings an answer can hold, as the rating controls record them.
const (
	AgentFeedbackHelpful  = "helpful"
	AgentFeedbackNotRight = "not-right"
)

// AgentRatingNotSavedText is the notice shown when a rating or its undo could
// not be saved and the previous rating was put back.
func AgentRatingNotSavedText(locale string) string {
	return agentReplyFallback(locale, "chat.agent.rating_not_saved", "Your rating was not saved. Try again.")
}

func agentFeedbackInvocation(model Model, postID string) string {
	for _, invocation := range model.PersonaInvocations {
		if invocation.Projection.DurablePostID == postID {
			return invocation.Projection.InvocationID
		}
	}
	return ""
}

func renderAgentFeedback(model Model, local localUI, postID string) ui.Node {
	invocationID := agentFeedbackInvocation(model, postID)
	if invocationID == "" {
		return nil
	}
	state := local.agentFeedback[invocationID]
	if restored, ok := model.AgentFeedbackRestored[invocationID]; ok {
		state = restored
	}
	helpful := agentReplyFallback(model.Locale, "chat.agent.helpful", "Helpful")
	notRight := agentReplyFallback(model.Locale, "chat.agent.not_right", "Not right")
	disabled := model.Callbacks.SubmitAgentFeedback == nil
	controls := html.Div(html.Props{Class: "agent-feedback", Role: "group", Aria: map[string]string{"label": agentReplyFallback(model.Locale, "chat.agent.feedback", "Rate this answer")}},
		html.Button(html.Props{Class: "agent-feedback-button", Type: "button", Disabled: disabled, Data: map[string]string{"action": "agent-feedback", "id": invocationID, "extra": "helpful"}, Aria: map[string]string{"label": helpful, "pressed": boolString(state == "helpful")}}, icon("thumb-up"), html.Span(html.Props{Text: helpful})),
		html.Button(html.Props{Class: "agent-feedback-button", Type: "button", Disabled: disabled, Data: map[string]string{"action": "agent-feedback", "id": invocationID, "extra": "not-right"}, Aria: map[string]string{"label": notRight, "pressed": boolString(state == "not-right")}}, icon("thumb-down"), html.Span(html.Props{Text: notRight})),
	)
	if state != "not-right" {
		return controls
	}
	name := personaAgentName(model, "")
	for _, invocation := range model.PersonaInvocations {
		if invocation.Projection.InvocationID == invocationID {
			name = personaAgentName(model, invocation.Projection.AgentName)
			break
		}
	}
	thanks := strings.ReplaceAll(agentReplyFallback(model.Locale, "chat.agent.feedback_owner_named", "Thanks. {name}'s owner has been told."), "{name}", name)
	return html.Div(html.Props{}, controls, html.P(html.Props{Class: "agent-feedback-sent", Role: "status"},
		html.Span(html.Props{Text: thanks}),
		html.Button(html.Props{Class: "agent-feedback-undo", Type: "button", Disabled: model.Callbacks.UndoAgentFeedback == nil, Data: map[string]string{"action": "agent-feedback-undo", "id": invocationID}, Text: agentReplyFallback(model.Locale, "chat.agent.undo", "Undo")})))
}

func agentFeedbackDirection(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "ar") {
		return "rtl"
	}
	return "ltr"
}
