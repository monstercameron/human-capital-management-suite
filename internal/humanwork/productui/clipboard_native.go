//go:build !js || !wasm

package productui

import "errors"

// copyToClipboard has no browser clipboard to write to outside a wasm
// build: SSR and native tests render the control the enhanced client wires
// up, but nothing here can act on a click. A caller that asks is told the
// copy did not happen.
func copyToClipboard(_ string, done ...func(error)) {
	for _, callback := range done {
		if callback != nil {
			callback(errors.New("clipboard unavailable"))
		}
	}
}
