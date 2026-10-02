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

// agentFeedbackState is the rating the controls show for an answer: what the
// server put back after a change that could not be saved; otherwise what the
// person chose on this page; otherwise their stored rating, as the server sent
// it with the agent activity, so a rating given before a reload is still shown
// after it (CHATBUG-066).
func agentFeedbackState(model Model, local localUI, invocationID string) string {
	if restored, ok := model.AgentFeedbackRestored[invocationID]; ok {
		return restored
	}
	if chosen := local.agentFeedback[invocationID]; chosen != "" {
		return chosen
	}
	return model.AgentFeedbackSaved[invocationID]
}

func renderAgentFeedback(model Model, local localUI, postID string) ui.Node {
	invocationID := agentFeedbackInvocation(model, postID)
	if invocationID == "" {
		return nil
	}
	state := agentFeedbackState(model, local, invocationID)
	helpful := agentReplyFallback(model.Locale, "chat.agent.helpful", "Helpful")
	notRight := agentReplyFallback(model.Locale, "chat.agent.not_right", "Not right")
	disabled := model.Callbacks.SubmitAgentFeedback == nil
	// CHATBUG-066: pressing the rating that is already filled removes it.
	action := func(rating string) string {
		if state == rating && model.Callbacks.UndoAgentFeedback != nil {
			return "agent-feedback-undo"
		}
		return "agent-feedback"
	}
	controls := html.Div(html.Props{Class: "agent-feedback", Role: "group", Aria: map[string]string{"label": agentReplyFallback(model.Locale, "chat.agent.feedback", "Rate this answer")}},
		html.Button(html.Props{Class: "agent-feedback-button", Type: "button", Title: helpful, Disabled: disabled, Data: map[string]string{"action": action(AgentFeedbackHelpful), "id": invocationID, "extra": "helpful"}, Aria: map[string]string{"label": helpful, "pressed": boolString(state == "helpful")}}, icon("thumb-up"), html.Span(html.Props{Text: helpful})),
		html.Button(html.Props{Class: "agent-feedback-button", Type: "button", Title: notRight, Disabled: disabled, Data: map[string]string{"action": action(AgentFeedbackNotRight), "id": invocationID, "extra": "not-right"}, Aria: map[string]string{"label": notRight, "pressed": boolString(state == "not-right")}}, icon("thumb-down"), html.Span(html.Props{Text: notRight})),
	)
	// AGENTUX-059: a rating the server did not take is not shown as given. The
	// controls are back at what the server holds, and the reason stands beside
	// them until the person rates again.
	if _, unsaved := model.AgentFeedbackRestored[invocationID]; unsaved {
		controls.Children = append(controls.Children, html.Span(html.Props{Class: "agent-feedback-unsaved", Role: "alert", Dir: "auto", Text: AgentRatingNotSavedText(model.Locale)}))
	}
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
