//go:build !js || !wasm

package chatui

func sendLaterFocusInside() bool { return false }
