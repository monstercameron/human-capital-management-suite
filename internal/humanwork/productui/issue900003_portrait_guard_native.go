//go:build !js || !wasm

package productui

// InstallPortraitGuard has nothing to watch outside a js/wasm client: static
// rendering cannot observe a failed image request.
func InstallPortraitGuard() {}
