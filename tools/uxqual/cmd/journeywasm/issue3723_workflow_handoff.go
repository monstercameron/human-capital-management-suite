package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// workflowHandoffState is the model's Start a promotion handoff for a viewer.
// It is offered only when the server resolved the promote-worker action as
// available to this viewer (the same verdict the global launcher uses), so it
// never leads to a flow that would turn the viewer away. Reaching the People
// page alone grants nothing: a page right is not a right to start work. The
// pages behind the handoff still decide what the viewer may start, for whom
// and with what. navigate is the shell's history router.
func workflowHandoffState(view productui.View, navigate func(string)) chatui.MessageWorkflowState {
	if navigate == nil || !view.CanStartPromotion() {
		return chatui.MessageWorkflowState{}
	}
	locale := view.Locale.Resolved
	return chatui.MessageWorkflowState{Enabled: true, Start: func(conversationID, messageID string) {
		if href := chatui.WorkflowHandoffHref(locale, conversationID, messageID); href != "" {
			navigate(href)
		}
	}}
}
