//go:build !(js && wasm)

package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// Outside the browser the split editor has no panes to keep in step: the
// server renders the empty fields and the controller's text is the whole
// state, so commands and history act on it directly.

func docsEditorMount(*docsEditorController) func() { return func() {} }
func docsEditorEnsure(*docsEditorController)       {}
func docsEditorNarrow() bool                       { return false }
func docsEditorFocus(string)                       {}
func docsEditorCommandAt(ui.Event) string          { return "" }
func docsEditorToolbarKey(ui.Event)                {}

func docsEditorCaptureSelection(c *docsEditorController) docsEditorCapture {
	return docsEditorCapture{Markdown: c.markdown, Start: len(c.markdown), End: len(c.markdown), Pane: c.pane}
}

func docsEditorCommit(c *docsEditorController, markdown string, _, _ int, _ string) {
	c.setMarkdown(markdown, false, false)
}

func docsEditorCommitHistory(c *docsEditorController, markdown string, _ int) {
	c.setMarkdown(markdown, false, true)
}
