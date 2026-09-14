//go:build js && wasm

package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// Mount renders the production component tree into the server shell. The
// shell owns the CSP-pinned stylesheet; this function owns only the DOM tree.
func Mount(view View, selector string) {
	ui.Render(Build(view), selector)
	bindOrganizationBrowseControls()
}
