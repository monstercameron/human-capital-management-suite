//go:build js && wasm

// This file is the live-browser entrypoint. It only builds under
// GOOS=js GOARCH=wasm because ui.Render mounts into the real DOM via
// syscall/js. tools/uxqual/cmd/uxqualwasm is the `go build` target that
// uses it.
package gwc

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"

	"github.com/monstercameron/human-capital-management-suite/internal/experience/workspacecontract"
)

// Mount renders the workspace live into the given CSS selector, injecting
// the shared stylesheet (via textContent, never innerHTML, so it is not an
// injection sink -- Stylesheet() is our own static, trusted string) exactly
// once into document.head first.
func Mount(c contract.WorkspaceContract, selector string) {
	injectStylesheet()
	ui.Render(Build(c), selector)
}

func injectStylesheet() {
	document := js.Global().Get("document")
	style := document.Call("createElement", "style")
	style.Set("textContent", Stylesheet())
	document.Get("head").Call("appendChild", style)
}
