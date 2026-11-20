//go:build !js || !wasm

package chatui

// ReadingPosition is the message at the top of the timeline and how far below
// the list's top edge it sits; ok is false when there is no view, or the reader
// is at the newest messages. There is no view outside the browser.
func ReadingPosition() (id string, offset int, ok bool) { return "", 0, false }

// RestoreReadingPosition puts the message back at the offset it was read at.
func RestoreReadingPosition(string, int, func()) {}

func focusChatTimeline() {}
