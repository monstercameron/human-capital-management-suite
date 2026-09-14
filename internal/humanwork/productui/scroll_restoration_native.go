//go:build !js || !wasm

package productui

// useScrollRestoration has no browser scroll position to restore outside a
// js/wasm client: static SSR rendering never scrolls, and every render
// starts a fresh document. See scroll_restoration_wasm.go for the real
// implementation.
func useScrollRestoration(id string, enabled bool) {}
