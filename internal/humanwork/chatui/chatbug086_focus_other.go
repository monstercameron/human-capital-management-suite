//go:build !js || !wasm

package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// The composer's focus handling needs a page; outside the browser a press does
// nothing, every input counts as typed, and there is no caret to hold.
func chatbug086Press(ui.Event) {}

func chatbug086Typed(ui.Event, string) bool { return true }

func chatbug086HoldCaret(string, int) {}
