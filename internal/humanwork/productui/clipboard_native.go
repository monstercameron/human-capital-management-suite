//go:build !js || !wasm

package productui

// copyToClipboard has no browser clipboard to write to outside a wasm
// build: SSR and native tests render the control the enhanced client wires
// up, but nothing here can act on a click.
func copyToClipboard(string) {}
