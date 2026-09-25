// Package productui: DOCS-07's recoverable removal flow. "Remove" never
// disposes a document; it withdraws the live default-scope deployment
// (WithdrawDocument), so the document stops being shared or discoverable
// by anyone but its owner while every immutable version stays intact. The
// confirm dialog says so up front, and the Undo toast that follows a
// successful removal (docsNotice.SetAction) calls RestoreDocument with
// the version that was live.
package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type docsRemoveDialogProps struct {
	Locale string
	Title  string
	// Remove performs the withdrawal; done reports only failure, since a
	// success closes the dialog and hands control to the caller's own
	// Undo toast.
	Remove func(done func(err error))
	Close  func()
}

// docsRemoveDialog confirms a recoverable removal before calling Remove.
// It never disposes anything and carries no destructive-looking styling
// beyond the one danger-toned confirm button; the explanatory copy is the
// safeguard, not a second confirmation step.
func docsRemoveDialog(props docsRemoveDialogProps) ui.Node {
	locale := props.Locale
	busy := ui.UseState(false)
	failed := ui.UseState(false)
	close := ui.UseEvent(func(ui.MouseEvent) {
		if !busy.Get() {
			props.Close()
		}
	})
	ui.UseEffect(func() func() { return docsListenEscape(props.Close) })
	useDocsModal(true, "docs-remove-dialog", "#docs-remove-confirm", docsDialogReturnFallbacks...)
	keydown := ui.UseEvent(func(ui.KeyboardEvent) {})
	confirm := ui.UseEvent(func(ui.MouseEvent) {
		if busy.Get() || props.Remove == nil {
			return
		}
		busy.Set(true)
		failed.Set(false)
		props.Remove(func(err error) {
			if err != nil {
				busy.Set(false)
				failed.Set(true)
			}
		})
	})
	text := func(key string) string { return docsText(locale, key) }
	body := []ui.Node{
		html.P(html.Props{Class: "docs-remove-title"}, ui.Text(props.Title)),
		html.P(html.Props{Class: "docs-remove-body"}, ui.Text(text("remove_body"))),
	}
	if failed.Get() {
		body = append(body, html.P(html.Props{Class: "docs-compare-status is-alert", Role: "alert"}, ui.Text(text("remove_failed"))))
	}
	body = append(body, html.Div(html.Props{Class: "docs-remove-actions"},
		html.Button(html.Props{Class: "button secondary", Type: "button", OnClick: close, Disabled: busy.Get()}, ui.Text(text("remove_cancel"))),
		// The confirm names the same action as the menu item that opened it
		// and is danger-toned like it, not the brand primary (D-10).
		html.Button(html.Props{ID: "docs-remove-confirm", Class: "button destructive", Type: "button", OnClick: confirm, Disabled: busy.Get()}, ui.Text(text("remove_action"))),
	))
	return docsDialog("docs-remove-dialog", text("remove_heading"), text("remove_cancel"), close, keydown, body...)
}

func docsRemoveStylesheet() string {
	return `
.docs-remove-title{font-weight:600}
.docs-remove-body{color:var(--muted)}
.docs-remove-actions{display:flex;justify-content:flex-end;gap:var(--hcm-space-2);margin-block-start:var(--hcm-space-2)}
`
}
