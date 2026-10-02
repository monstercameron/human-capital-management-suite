//go:build !js || !wasm

package chatui

// Outside the browser there is no page to draw the "/" list in or to press
// Send on; the list's state and its rendering are checked without them.

func commandMenuShow(string, []string, int) {}
func commandMenuSubmit(string)              {}
