//go:build !js || !wasm

package chatui

func focusHandoverTarget(string) {}
func focusHandoverTrigger()      {}
func handoverFocusInside() bool  { return false }

// FocusHandoverText puts the caret in the prepared brief once it is drawn.
func FocusHandoverText() {}
