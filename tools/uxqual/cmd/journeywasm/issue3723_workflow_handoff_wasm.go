//go:build js && wasm

package main

import (
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

// workflowHandoffProject gives the page's model the Start a promotion row of
// the message menu and /promote (issue 3723), for a viewer whose workspace
// reaches the promotion flow.
func workflowHandoffProject(model chatui.Model, view productui.View) chatui.Model {
	model.MessageWorkflow = workflowHandoffState(view, view.Navigate)
	return model
}
