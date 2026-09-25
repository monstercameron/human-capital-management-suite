//go:build !js || !wasm

package chatui

// revealSelectedRailRow is a no-op on the native build; see rail_focus_js.go
// (C-9).
func revealSelectedRailRow(string) {}
