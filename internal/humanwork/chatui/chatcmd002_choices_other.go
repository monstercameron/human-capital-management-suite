//go:build !js || !wasm

package chatui

// chatcmd002CheckedChoices has no page to read outside the browser.
func chatcmd002CheckedChoices(string) []string { return nil }

func chatcmd002RevealMessage(string) {}
