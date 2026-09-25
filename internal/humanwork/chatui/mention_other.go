//go:build !js || !wasm

package chatui

func composerSelection(string) (string, int, bool) { return "", 0, false }
func replaceComposerText(string, string, int)      {}

// positionMentionMenu is a no-op on the native build; see mention_js.go
// (C-19).
func positionMentionMenu(string) {}
