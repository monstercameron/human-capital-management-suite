//go:build !js || !wasm

package chatui

func composerSelection(string) (string, int, bool) { return "", 0, false }
func replaceComposerText(string, string, int)      {}
