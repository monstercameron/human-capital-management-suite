//go:build !js || !wasm

package productui

func useUXBlindQPopoverController() {}

func useUXBlindQPopoverDismissal(_ string, _ bool, _ func(), _ string) {}
