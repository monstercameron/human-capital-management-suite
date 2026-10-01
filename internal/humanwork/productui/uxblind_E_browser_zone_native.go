//go:build !js || !wasm

package productui

// Native rendering has no browser clock. SSR keeps the established stable
// morning fallback; the WASM render resolves the device zone on hydration.
func browserLocalHour() (int, bool) { return 0, false }
