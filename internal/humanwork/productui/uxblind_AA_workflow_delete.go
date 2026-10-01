package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func workflowDraftDeleteConfirmation(i18n I18nProps, open bool, cancel, confirm ui.Handler) ui.Node {
	if !open {
		return nil
	}
	return html.Div(html.Props{Class: "workflow-draft-delete-backdrop"},
		html.Section(html.Props{
			ID: "workflow-draft-delete-dialog", Class: "surface workflow-draft-delete-confirmation",
			Raw: map[string]any{"role": "alertdialog", "aria-modal": "true", "aria-labelledby": "workflow-draft-delete-title", "aria-describedby": "workflow-draft-delete-detail", "tabindex": "-1"},
		},
			html.H2(html.Props{ID: "workflow-draft-delete-title"}, ui.Text(i18n.Text("workflow_draft.delete_title"))),
			html.P(html.Props{ID: "workflow-draft-delete-detail"}, ui.Text(i18n.Text("workflow_draft.delete_detail"))),
			html.Div(html.Props{Class: "workflow-draft-delete-actions"},
				html.Button(html.Props{Class: "button secondary compact", Type: "button", Data: map[string]string{"workflow-action": "delete-draft-cancel"}, OnClick: cancel}, ui.Text(i18n.Text("workflow_draft.delete_cancel"))),
				html.Button(html.Props{Class: "button danger compact", Type: "button", Data: map[string]string{"workflow-action": "delete-draft-confirm"}, OnClick: confirm}, ui.Text(i18n.Text("workflow_draft.delete_confirm"))),
			),
		),
	)
}

// useWorkflowDraftDeleteFocus gives the delete confirmation the shared dialog
// focus contract (UXLIVE-020): focus moves into it when it opens and returns
// to the trigger that opened it. It lives beside the dialog it serves.
func useWorkflowDraftDeleteFocus(open bool) {
	useDrawerFocusTrap("workflow-draft-delete-dialog", "workflow-draft-delete-trigger", open)
}
