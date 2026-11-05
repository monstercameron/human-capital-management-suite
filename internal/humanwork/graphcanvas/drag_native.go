//go:build !js || !wasm

package graphcanvas

// Native stubs: outside a browser there is no DOM to transform.
func applyNodeTransform(_ string, _, _ float64) {}
func clearNodeTransform(_ string)               {}
