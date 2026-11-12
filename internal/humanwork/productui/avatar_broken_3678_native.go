//go:build !js || !wasm

package productui

// useBrokenPortraitSweep has no loaded image to inspect outside a js/wasm
// client; static rendering cannot observe a failed request.
func useBrokenPortraitSweep(photoURL string, onBroken func()) {}
