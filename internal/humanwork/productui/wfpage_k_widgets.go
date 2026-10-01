package productui

import (
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/pagedef"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/widgetreg"
)

// WorkflowInputRenderer is the product-ui boundary for workflow input
// widgets. It delegates the closed renderer vocabulary to widgetreg and keeps
// page composition free of per-field switches.
type WorkflowInputRenderer struct {
	registry widgetreg.WorkflowInputRegistry
}

// NewWorkflowInputRenderer returns a renderer with an isolated registry.
func NewWorkflowInputRenderer() WorkflowInputRenderer {
	return WorkflowInputRenderer{registry: widgetreg.NewWorkflowInputRegistry()}
}

// RenderWorkflowInput projects one authorized page widget into the semantic
// view consumed by the product renderer.
func (r WorkflowInputRenderer) RenderWorkflowInput(req widgetreg.InputRenderRequest) (widgetreg.InputRenderResult, error) {
	return r.registry.Render(req)
}

// WorkflowInputWidgetKind is provided at the product-ui seam so callers do
// not need to invent a second kind vocabulary.
type WorkflowInputWidgetKind = pagedef.WorkflowWidgetKind
