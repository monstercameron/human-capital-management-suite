//go:build !js || !wasm

package chatui

// There is no page to move focus in outside the browser.
func chatissue3877Focus(string) {}
