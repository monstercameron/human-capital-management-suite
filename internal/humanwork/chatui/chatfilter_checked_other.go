//go:build !js || !wasm

package chatui

func filterChecked(string) bool { return false }

func filterSetChecked(string, bool) {}
