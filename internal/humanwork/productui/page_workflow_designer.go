package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// workflowDesignerPage is the route adapter. It narrows the broad page view
// to the workflow editor's transport-neutral component props.
func workflowDesignerPage(view View) ui.Node {
	return ui.CreateElement(WorkflowDesignerPage, WorkflowDesignerPageProps{
		I18nProps:      I18nProps{Locale: view.Locale},
		Catalog:        append([]WorkflowCatalogItem(nil), view.PublishedWorkflows...),
		Palette:        append([]WorkflowPaletteItem(nil), view.WorkflowPalette...),
		Selected:       view.WorkflowView,
		Draft:          view.WorkflowDraft,
		BaseHref:       statefulHref(view, PageWorkflowDesigner),
		Navigate:       view.Navigate,
		CanCreate:      view.CanFeature(PageWorkflowDesigner, "workflow_drafts", "create"),
		OnCreate:       view.CreateWorkflowDraft,
		OnInsert:       view.InsertWorkflowPaletteEntry,
		OnUpdateNode:   view.UpdateWorkflowDraftNode,
		OnSetOutcome:   view.SetWorkflowDraftOutcome,
		OnBindInput:    view.BindWorkflowDraftInput,
		OnMoveNode:     view.MoveWorkflowDraftNode,
		OnOverlay:      view.ApplyWorkflowOverlay,
		OnHistory:      view.NavigateWorkflowDraftHistory,
		SelectedNodeID: view.SelectedWorkflowNodeID,
		OnSelectNode:   view.SelectWorkflowDraftNode,
	})
}
