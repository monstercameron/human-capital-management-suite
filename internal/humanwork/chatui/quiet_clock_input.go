package chatui

import "strings"

// quietClockInput reads a quiet-hours time field. A cleared field carries no
// time, so it is not a boundary: ok is false and the caller keeps the saved
// minute. parseClock("") would otherwise return 0, silently saving midnight
// while the field reads empty.
func quietClockInput(value string) (int, bool) {
	if strings.TrimSpace(value) == "" {
		return 0, false
	}
	return parseClock(value), true
}
